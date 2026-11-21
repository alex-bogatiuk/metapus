// Package crypto_resource_rental models provider-backed blockchain resource leases.
package crypto_resource_rental

import (
	"context"
	"time"

	"metapus/internal/configuration/crypto/domain/registers/crypto_platform_cost"
	"metapus/internal/configuration/crypto/domain/registers/crypto_provider_balance"
	"metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

type Status string

const (
	StatusCreated    Status = "created"
	StatusOrdering   Status = "ordering"
	StatusDelegated  Status = "delegated"
	StatusActive     Status = "active"
	StatusReclaiming Status = "reclaiming"
	StatusClosed     Status = "closed"
	StatusFailed     Status = "failed"
	StatusHeld       Status = "held"
)

type CostStatus string

const (
	CostStatusUnknown           CostStatus = "unknown"
	CostStatusRecorded          CostStatus = "recorded"
	CostStatusPartiallyRefunded CostStatus = "partially_refunded"
	CostStatusRefunded          CostStatus = "refunded"
	CostStatusHeld              CostStatus = "held"
)

// CryptoResourceRental is a system-generated document for one provider resource lease.
type CryptoResourceRental struct {
	entity.Document

	ProviderAccountID    id.ID            `db:"provider_account_id" json:"providerAccountId" meta:"label:Аккаунт провайдера,ref:crypto_provider_account"`
	NetworkID            id.ID            `db:"network_id" json:"networkId" meta:"label:Сеть,ref:blockchain_network"`
	ReceiveWalletID      id.ID            `db:"receive_wallet_id" json:"receiveWalletId" meta:"label:Кошелёк,ref:wallet"`
	CostTokenID          id.ID            `db:"cost_token_id" json:"costTokenId" meta:"label:Токен расхода,ref:token"`
	ResourceType         string           `db:"resource_type" json:"resourceType" meta:"label:Ресурс"`
	RentalMode           string           `db:"rental_mode" json:"rentalMode" meta:"label:Режим"`
	RentalPeriod         string           `db:"rental_period" json:"rentalPeriod" meta:"label:Период"`
	RequestedAmount      int64            `db:"requested_amount" json:"requestedAmount" meta:"label:Запрошено,type:integer"`
	DelegatedAmount      int64            `db:"delegated_amount" json:"delegatedAmount" meta:"label:Делегировано,type:integer"`
	ReservedAmount       int64            `db:"reserved_amount" json:"reservedAmount" meta:"label:Зарезервировано,type:integer"`
	ConsumedAmount       int64            `db:"consumed_amount" json:"consumedAmount" meta:"label:Потреблено,type:integer"`
	GrossCostAmount      types.RawAmount  `db:"gross_cost_amount" json:"grossCostAmount" meta:"label:Стоимость"`
	RefundAmount         types.RawAmount  `db:"refund_amount" json:"refundAmount" meta:"label:Возврат"`
	NetCostAmount        types.RawAmount  `db:"net_cost_amount" json:"netCostAmount" meta:"label:Чистая стоимость"`
	AllocatedCostAmount  types.RawAmount  `db:"allocated_cost_amount" json:"allocatedCostAmount" meta:"label:Распределено"`
	UnusedCostAmount     types.RawAmount  `db:"unused_cost_amount" json:"unusedCostAmount" meta:"label:Не использовано"`
	Provider             string           `db:"provider" json:"provider" meta:"label:Провайдер"`
	OutTradeNo           string           `db:"out_trade_no" json:"outTradeNo" meta:"label:Внешний ключ"`
	ExternalSerial       string           `db:"external_serial" json:"externalSerial,omitempty" meta:"label:Заказ провайдера"`
	Status               Status           `db:"status" json:"status" meta:"label:Статус"`
	CostStatus           CostStatus       `db:"cost_status" json:"costStatus" meta:"label:Учет стоимости"`
	ProviderBalanceAfter *types.RawAmount `db:"provider_balance_after" json:"providerBalanceAfter,omitempty" meta:"label:Баланс после"`
	RequestJSON          []byte           `db:"request_json" json:"-"`
	ResponseJSON         []byte           `db:"response_json" json:"-"`
	ErrorMessage         string           `db:"error_message" json:"errorMessage,omitempty" meta:"label:Ошибка"`
	DelegatedAt          *time.Time       `db:"delegated_at" json:"delegatedAt,omitempty"`
	ActivatedAt          *time.Time       `db:"activated_at" json:"activatedAt,omitempty"`
	ReclaimRequestedAt   *time.Time       `db:"reclaim_requested_at" json:"reclaimRequestedAt,omitempty"`
	ExpiresAt            *time.Time       `db:"expires_at" json:"expiresAt,omitempty"`
	ClosedAt             *time.Time       `db:"closed_at" json:"closedAt,omitempty"`
	ClaimedBy            string           `db:"claimed_by" json:"-"`
	ClaimedAt            *time.Time       `db:"claimed_at" json:"-"`
	LeaseExpiresAt       *time.Time       `db:"lease_expires_at" json:"-"`
}

func New(providerAccountID, networkID, receiveWalletID, costTokenID id.ID, provider, outTradeNo string, requested int64) *CryptoResourceRental {
	return &CryptoResourceRental{
		Document:          entity.NewDocument(),
		ProviderAccountID: providerAccountID,
		NetworkID:         networkID,
		ReceiveWalletID:   receiveWalletID,
		CostTokenID:       costTokenID,
		ResourceType:      "energy",
		RentalMode:        "fixed",
		RentalPeriod:      "1H",
		RequestedAmount:   requested,
		Provider:          provider,
		OutTradeNo:        outTradeNo,
		Status:            StatusCreated,
		CostStatus:        CostStatusUnknown,
	}
}

func (r *CryptoResourceRental) Validate(ctx context.Context) error {
	if err := r.Document.Validate(ctx); err != nil {
		return err
	}
	if id.IsNil(r.ProviderAccountID) || id.IsNil(r.NetworkID) || id.IsNil(r.ReceiveWalletID) || id.IsNil(r.CostTokenID) {
		return apperror.NewValidation("rental references are required")
	}
	if r.Provider == "" || r.OutTradeNo == "" {
		return apperror.NewValidation("provider order identity is required")
	}
	if r.RequestedAmount <= 0 {
		return apperror.NewValidation("requested resource amount must be positive").WithDetail("field", "requestedAmount")
	}
	return nil
}

func (r *CryptoResourceRental) GetDocumentType() string { return Definition.Name }

func (r *CryptoResourceRental) GenerateCryptoPlatformCostMovements(context.Context) ([]crypto_platform_cost.CryptoPlatformCostMovement, error) {
	if !r.GrossCostAmount.IsPositive() {
		return nil, nil
	}
	return []crypto_platform_cost.CryptoPlatformCostMovement{crypto_platform_cost.NewSharedCryptoPlatformCostMovement(
		r.ID, r.GetDocumentType(), r.PostedVersion+1, r.Date, entity.RecordTypeExpense,
		r.NetworkID, r.CostTokenID, crypto_platform_cost.PlatformCostTypeChainResourceRent,
		crypto_platform_cost.PlatformCostPurposeResourceRental, r.Provider, r.ExternalSerial, r.GrossCostAmount,
	)}, nil
}

func (r *CryptoResourceRental) GenerateCryptoProviderBalanceMovements(context.Context) ([]crypto_provider_balance.CryptoProviderBalanceMovement, error) {
	if !r.GrossCostAmount.IsPositive() {
		return nil, nil
	}
	return []crypto_provider_balance.CryptoProviderBalanceMovement{crypto_provider_balance.NewCryptoProviderBalanceMovement(
		r.ID, r.GetDocumentType(), r.PostedVersion+1, r.Date, entity.RecordTypeExpense,
		r.ProviderAccountID, r.CostTokenID, "rental_charge", r.ExternalSerial, r.GrossCostAmount,
	)}, nil
}

var _ posting.Postable = (*CryptoResourceRental)(nil)
var _ cryptoposting.CryptoPlatformCostMovementSource = (*CryptoResourceRental)(nil)
var _ cryptoposting.CryptoProviderBalanceMovementSource = (*CryptoResourceRental)(nil)
