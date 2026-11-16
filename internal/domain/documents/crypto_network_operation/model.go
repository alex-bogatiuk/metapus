// Package crypto_network_operation models system-owned blockchain operations
// that have an accounting effect after network finality.
package crypto_network_operation

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/internal/domain/registers/cryptoposting"
)

const (
	DocumentType            = "CryptoNetworkOperation"
	ActivationTaskBasisType = "CryptoAccountActivationTask"
)

// OperationType is deliberately closed. New blockchain operations must add
// their own validation and movement-generation rules before they can post.
type OperationType string

const OperationTypeWalletActivation OperationType = "wallet_activation"

func (t OperationType) Valid() bool { return t == OperationTypeWalletActivation }

type Status string

const (
	StatusBroadcast Status = "broadcast"
	StatusSucceeded Status = "succeeded"
	StatusReverted  Status = "reverted"
)

func (s Status) Valid() bool {
	switch s {
	case StatusBroadcast, StatusSucceeded, StatusReverted:
		return true
	default:
		return false
	}
}

func (s Status) Final() bool { return s == StatusSucceeded || s == StatusReverted }

// CryptoNetworkOperation is the accounting document for an accepted network
// transaction. Worker state and retries live in reg_crypto_account_activations.
// It is system-owned: the activation workflow creates it at broadcast and
// posts it once at finality together with its recognized network fee.
type CryptoNetworkOperation struct {
	entity.Document

	OperationType           OperationType   `db:"operation_type" json:"operationType" meta:"label:Операция"`
	Status                  Status          `db:"status" json:"status" meta:"label:Статус"`
	NetworkID               id.ID           `db:"network_id" json:"networkId" meta:"label:Сеть,ref:blockchain_network"`
	TargetWalletID          id.ID           `db:"target_wallet_id" json:"targetWalletId" meta:"label:Целевой кошелёк,ref:wallet"`
	TargetAddress           string          `db:"target_address" json:"targetAddress" meta:"label:Адрес назначения"`
	SourceWalletID          id.ID           `db:"source_wallet_id" json:"sourceWalletId" meta:"label:Кошелёк списания,ref:wallet"`
	CostTokenID             id.ID           `db:"cost_token_id" json:"costTokenId" meta:"label:Токен расхода,ref:token"`
	CostAmount              types.RawAmount `db:"cost_amount" json:"costAmount" meta:"label:Сумма расхода"`
	TxHash                  string          `db:"tx_hash" json:"txHash" meta:"label:Хеш транзакции"`
	ChainSettlementID       *id.ID          `db:"chain_settlement_id" json:"chainSettlementId,omitempty" meta:"label:Сетевой расчёт"`
	NetworkFeeReservationID *id.ID          `db:"network_fee_reservation_id" json:"networkFeeReservationId,omitempty"`
	FinalizedAt             *time.Time      `db:"finalized_at" json:"finalizedAt,omitempty" meta:"label:Дата финальности"`
}

func NewWalletActivation(
	activationID, networkID, targetWalletID, sourceWalletID, costTokenID id.ID,
	targetAddress, txHash string,
) *CryptoNetworkOperation {
	doc := &CryptoNetworkOperation{
		Document:       entity.NewDocument(),
		OperationType:  OperationTypeWalletActivation,
		Status:         StatusBroadcast,
		NetworkID:      networkID,
		TargetWalletID: targetWalletID,
		TargetAddress:  strings.TrimSpace(targetAddress),
		SourceWalletID: sourceWalletID,
		CostTokenID:    costTokenID,
		TxHash:         strings.TrimSpace(txHash),
	}
	doc.BasisType = ActivationTaskBasisType
	doc.BasisID = &activationID
	return doc
}

func (d *CryptoNetworkOperation) Validate(ctx context.Context) error {
	if err := d.Document.Validate(ctx); err != nil {
		return err
	}
	if !d.OperationType.Valid() {
		return apperror.NewValidation("unsupported crypto network operation type").WithDetail("field", "operationType")
	}
	if !d.Status.Valid() {
		return apperror.NewValidation("invalid crypto network operation status").WithDetail("field", "status")
	}
	if d.OperationType == OperationTypeWalletActivation &&
		(d.BasisType != ActivationTaskBasisType || d.BasisID == nil || id.IsNil(*d.BasisID)) {
		return apperror.NewValidation("wallet activation task basis is required").WithDetail("field", "basisId")
	}
	if id.IsNil(d.NetworkID) || id.IsNil(d.TargetWalletID) || id.IsNil(d.SourceWalletID) || id.IsNil(d.CostTokenID) {
		return apperror.NewValidation("crypto network operation references are required")
	}
	if strings.TrimSpace(d.TargetAddress) == "" || strings.TrimSpace(d.TxHash) == "" {
		return apperror.NewValidation("network transaction identity is required")
	}
	if d.Status.Final() && (d.ChainSettlementID == nil || id.IsNil(*d.ChainSettlementID) || d.FinalizedAt == nil || d.FinalizedAt.IsZero() ||
		d.NetworkFeeReservationID == nil || id.IsNil(*d.NetworkFeeReservationID)) {
		return apperror.NewValidation("final network operation requires its settlement, fee reservation and finality date")
	}
	return nil
}

func (d *CryptoNetworkOperation) CanPost(ctx context.Context) error {
	if err := d.Validate(ctx); err != nil {
		return err
	}
	if !d.Status.Final() {
		return apperror.NewBusinessRule("CRYPTO_NETWORK_OPERATION_NOT_FINAL", "crypto network operation is not final")
	}
	return d.Document.CanPost(ctx)
}

func (d *CryptoNetworkOperation) GetDocumentType() string { return DocumentType }

func (d *CryptoNetworkOperation) GenerateCryptoBalanceMovements(context.Context) ([]crypto_balance.CryptoBalanceMovement, error) {
	if err := d.validateMovementOperation(); err != nil {
		return nil, err
	}
	if !d.CostAmount.IsPositive() {
		return nil, nil
	}
	movement := crypto_balance.NewCryptoBalanceMovement(
		d.ID, d.GetDocumentType(), d.PostedVersion+1, *d.FinalizedAt, entity.RecordTypeExpense,
		d.SourceWalletID, d.CostTokenID, d.CostAmount,
	)
	movement.LineID = uuid.NewSHA1(d.ID, []byte("network-fee-expense"))
	return []crypto_balance.CryptoBalanceMovement{movement}, nil
}

func (d *CryptoNetworkOperation) GenerateCryptoPlatformCostMovements(context.Context) ([]crypto_platform_cost.CryptoPlatformCostMovement, error) {
	if err := d.validateMovementOperation(); err != nil {
		return nil, err
	}
	if !d.CostAmount.IsPositive() {
		return nil, nil
	}
	movement := crypto_platform_cost.NewSharedCryptoPlatformCostMovement(
		d.ID, d.GetDocumentType(), d.PostedVersion+1, *d.FinalizedAt, entity.RecordTypeExpense,
		d.NetworkID, d.CostTokenID, crypto_platform_cost.PlatformCostTypeChainNetworkFee,
		crypto_platform_cost.PlatformCostPurposeActivation, "chain", d.TxHash, d.CostAmount,
	)
	movement.LineID = uuid.NewSHA1(d.ID, []byte("network-fee-cost"))
	return []crypto_platform_cost.CryptoPlatformCostMovement{movement}, nil
}

func (d *CryptoNetworkOperation) CanUnpost(context.Context) error {
	return apperror.NewConflict("recognized network fee requires explicit compensation workflow")
}

// validateMovementOperation admits movements only of a supported operation
// that reached finality: its fee is recognized in the final period.
func (d *CryptoNetworkOperation) validateMovementOperation() error {
	if d.OperationType != OperationTypeWalletActivation {
		return apperror.NewValidation("unsupported crypto network operation type").WithDetail("field", "operationType")
	}
	if d.FinalizedAt == nil || d.FinalizedAt.IsZero() {
		return apperror.NewValidation("network fee requires its final period")
	}
	return nil
}

var _ posting.Postable = (*CryptoNetworkOperation)(nil)
var _ cryptoposting.CryptoBalanceMovementSource = (*CryptoNetworkOperation)(nil)
var _ cryptoposting.CryptoPlatformCostMovementSource = (*CryptoNetworkOperation)(nil)
var _ posting.UnpostGuard = (*CryptoNetworkOperation)(nil)
