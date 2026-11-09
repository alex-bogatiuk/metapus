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
type CryptoNetworkOperation struct {
	entity.Document

	OperationType           OperationType      `db:"operation_type" json:"operationType" meta:"label:Операция"`
	Status                  Status             `db:"status" json:"status" meta:"label:Статус"`
	NetworkID               id.ID              `db:"network_id" json:"networkId" meta:"label:Сеть,ref:blockchain_network"`
	TargetWalletID          id.ID              `db:"target_wallet_id" json:"targetWalletId" meta:"label:Целевой кошелёк,ref:wallet"`
	TargetAddress           string             `db:"target_address" json:"targetAddress" meta:"label:Адрес назначения"`
	SourceWalletID          id.ID              `db:"source_wallet_id" json:"sourceWalletId" meta:"label:Кошелёк списания,ref:wallet"`
	CostTokenID             id.ID              `db:"cost_token_id" json:"costTokenId" meta:"label:Токен расхода,ref:token"`
	CostAmount              types.CryptoAmount `db:"cost_amount" json:"costAmount" meta:"label:Сумма расхода"`
	TxHash                  string             `db:"tx_hash" json:"txHash" meta:"label:Хеш транзакции"`
	ChainSettlementID       *id.ID             `db:"chain_settlement_id" json:"chainSettlementId,omitempty" meta:"label:Сетевой расчёт"`
	NetworkFeeReservationID *id.ID             `db:"network_fee_reservation_id" json:"networkFeeReservationId,omitempty"`
	FinalizedAt             *time.Time         `db:"finalized_at" json:"finalizedAt,omitempty" meta:"label:Дата финальности"`
	ManualEditAllowed       bool               `db:"manual_edit_allowed" json:"manualEditAllowed" meta:"label:Редактирование разрешено"`
}

func NewWalletActivation(
	activationID, networkID, targetWalletID, sourceWalletID, costTokenID id.ID,
	targetAddress, txHash string,
) *CryptoNetworkOperation {
	doc := &CryptoNetworkOperation{
		Document:          entity.NewDocument(),
		OperationType:     OperationTypeWalletActivation,
		Status:            StatusBroadcast,
		NetworkID:         networkID,
		TargetWalletID:    targetWalletID,
		TargetAddress:     strings.TrimSpace(targetAddress),
		SourceWalletID:    sourceWalletID,
		CostTokenID:       costTokenID,
		TxHash:            strings.TrimSpace(txHash),
		ManualEditAllowed: false,
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
	if d.Status.Final() && (d.ChainSettlementID == nil || id.IsNil(*d.ChainSettlementID) || d.FinalizedAt == nil || d.FinalizedAt.IsZero()) {
		return apperror.NewValidation("final network operation requires settlement and finality date")
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

func (d *CryptoNetworkOperation) IsManualEditAllowed() bool { return d.ManualEditAllowed }
func (d *CryptoNetworkOperation) SetManualEditAllowed(allowed bool) {
	d.ManualEditAllowed = allowed
}

func (d *CryptoNetworkOperation) GenerateCryptoBalanceMovements(context.Context) ([]entity.CryptoBalanceMovement, error) {
	if err := d.validateMovementOperation(); err != nil {
		return nil, err
	}
	if !d.CostAmount.IsPositive() {
		return nil, nil
	}
	movement := entity.NewCryptoBalanceMovement(
		d.ID, d.GetDocumentType(), d.PostedVersion+1, d.Date, entity.RecordTypeExpense,
		d.SourceWalletID, d.CostTokenID, d.CostAmount,
	)
	if d.NetworkFeeReservationID != nil {
		if d.FinalizedAt == nil {
			return nil, apperror.NewValidation("network fee requires its final period")
		}
		movement.LineID = uuid.NewSHA1(d.ID, []byte("network-fee-expense"))
		movement.Period = *d.FinalizedAt
	}
	return []entity.CryptoBalanceMovement{movement}, nil
}

func (d *CryptoNetworkOperation) GenerateCryptoPlatformCostMovements(context.Context) ([]entity.CryptoPlatformCostMovement, error) {
	if err := d.validateMovementOperation(); err != nil {
		return nil, err
	}
	if !d.CostAmount.IsPositive() {
		return nil, nil
	}
	movement := entity.NewSharedCryptoPlatformCostMovement(
		d.ID, d.GetDocumentType(), d.PostedVersion+1, d.Date, entity.RecordTypeExpense,
		d.NetworkID, d.CostTokenID, entity.PlatformCostTypeChainNetworkFee,
		entity.PlatformCostPurposeActivation, "chain", d.TxHash, d.CostAmount,
	)
	if d.NetworkFeeReservationID != nil {
		if d.FinalizedAt == nil {
			return nil, apperror.NewValidation("network fee requires its final period")
		}
		movement.LineID = uuid.NewSHA1(d.ID, []byte("network-fee-cost"))
		movement.Period = *d.FinalizedAt
	}
	return []entity.CryptoPlatformCostMovement{movement}, nil
}

func (d *CryptoNetworkOperation) CanUnpost(context.Context) error {
	if d.NetworkFeeReservationID != nil {
		return apperror.NewConflict("recognized network fee requires explicit compensation workflow")
	}
	return nil
}

func (d *CryptoNetworkOperation) validateMovementOperation() error {
	if d.OperationType != OperationTypeWalletActivation {
		return apperror.NewValidation("unsupported crypto network operation type").WithDetail("field", "operationType")
	}
	return nil
}

var _ posting.Postable = (*CryptoNetworkOperation)(nil)
var _ posting.CryptoBalanceMovementSource = (*CryptoNetworkOperation)(nil)
var _ posting.CryptoPlatformCostMovementSource = (*CryptoNetworkOperation)(nil)
