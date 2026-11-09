// Package treasury_withdrawal provides the TreasuryWithdrawal document.
// TreasuryWithdrawal records platform-owned crypto revenue withdrawals.
package treasury_withdrawal

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/domain/posting"
)

// Status represents the lifecycle state of a treasury withdrawal.
type Status string

const DocumentType = "TreasuryWithdrawal"

const (
	StatusPendingSignature Status = "pending_signature"
	StatusPendingApproval  Status = "pending_approval"
	StatusApproved         Status = "approved"
	StatusSigning          Status = "signing"
	StatusBroadcast        Status = "broadcast"
	StatusConfirmed        Status = "confirmed"
	StatusHeld             Status = "held"
	StatusRejected         Status = "rejected"
	StatusFailed           Status = "failed"
)

// IsTerminal returns true when no worker processing should continue.
func (s Status) IsTerminal() bool {
	return s == StatusConfirmed || s == StatusRejected || s == StatusFailed
}

// TreasuryWithdrawal is an admin-only withdrawal of platform-owned revenue.
type TreasuryWithdrawal struct {
	entity.Document

	TokenID                  id.ID                           `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`
	SourceWalletID           id.ID                           `db:"source_wallet_id" json:"sourceWalletId" meta:"label:Исходный кошелёк,ref:wallet"`
	DestAddress              string                          `db:"dest_address" json:"destAddress" meta:"label:Адрес назначения"`
	AddressID                id.ID                           `db:"address_id" json:"addressId" meta:"label:Адрес назначения,ref:treasury_withdrawal_address"`
	Amount                   types.CryptoAmount              `db:"amount" json:"amount" meta:"label:Сумма"`
	NetworkFee               types.CryptoAmount              `db:"network_fee" json:"networkFee" meta:"label:Комиссия сети"`
	NetworkFeeTokenID        *id.ID                          `db:"network_fee_token_id" json:"networkFeeTokenId,omitempty" meta:"label:Токен комиссии,ref:token"`
	TxHash                   string                          `db:"tx_hash" json:"txHash,omitempty" meta:"label:TX Hash"`
	Status                   Status                          `db:"status" json:"status" meta:"label:Статус"`
	ApprovedBy               *id.ID                          `db:"approved_by" json:"approvedBy,omitempty" meta:"label:Утвердил,ref:user"`
	ApprovedAt               *time.Time                      `db:"approved_at" json:"approvedAt,omitempty" meta:"label:Утверждено"`
	RejectionReason          string                          `db:"rejection_reason" json:"rejectionReason,omitempty" meta:"label:Причина отказа"`
	HeldReason               string                          `db:"held_reason" json:"heldReason,omitempty" meta:"label:Причина удержания"`
	SettlementStatus         outbound_settlement.Status      `db:"settlement_status" json:"settlementStatus"`
	SettlementCheckedAt      *time.Time                      `db:"settlement_checked_at" json:"settlementCheckedAt,omitempty"`
	SettlementError          string                          `db:"settlement_error" json:"settlementError,omitempty"`
	SettlementClaimedBy      string                          `db:"settlement_claimed_by" json:"-"`
	SettlementClaimedAt      *time.Time                      `db:"settlement_claimed_at" json:"-"`
	SettlementLeaseExpiresAt *time.Time                      `db:"settlement_lease_expires_at" json:"-"`
	ChainResult              outbound_settlement.ChainResult `db:"chain_result" json:"chainResult,omitempty"`
	EnergyUsed               int64                           `db:"energy_used" json:"energyUsed"`
	NetFee                   types.CryptoAmount              `db:"net_fee" json:"netFee"`
	EnergyFee                types.CryptoAmount              `db:"energy_fee" json:"energyFee"`
	NetUsage                 int64                           `db:"net_usage" json:"netUsage"`

	FeeLines []FeeLine `db:"-" json:"feeLines,omitempty" meta:"label:Источники комиссии"`
}

// FeeLine identifies platform-revenue balance consumed by a treasury withdrawal.
type FeeLine struct {
	MerchantID id.ID              `json:"merchantId"`
	TokenID    id.ID              `json:"tokenId"`
	FeeType    entity.FeeType     `json:"feeType"`
	Amount     types.CryptoAmount `json:"amount"`
}

// NewTreasuryWithdrawal creates a pending admin treasury withdrawal.
func NewTreasuryWithdrawal(tokenID, sourceWalletID, addressID id.ID, destAddress string, amount types.CryptoAmount) *TreasuryWithdrawal {
	return &TreasuryWithdrawal{
		Document:         entity.NewDocument(),
		TokenID:          tokenID,
		SourceWalletID:   sourceWalletID,
		AddressID:        addressID,
		DestAddress:      destAddress,
		Amount:           amount,
		NetworkFee:       types.ZeroCryptoAmount(),
		Status:           StatusPendingApproval,
		SettlementStatus: outbound_settlement.StatusNotApplicable,
	}
}

// Validate implements entity.Validatable.
func (w *TreasuryWithdrawal) Validate(ctx context.Context) error {
	if err := w.Document.Validate(ctx); err != nil {
		return err
	}
	if id.IsNil(w.TokenID) {
		return apperror.NewValidation("token is required").WithDetail("field", "tokenId")
	}
	if id.IsNil(w.SourceWalletID) {
		return apperror.NewValidation("source wallet is required").WithDetail("field", "sourceWalletId")
	}
	if id.IsNil(w.AddressID) {
		return apperror.NewValidation("treasury address is required").WithDetail("field", "addressId")
	}
	if w.DestAddress == "" {
		return apperror.NewValidation("destination address is required").WithDetail("field", "destAddress")
	}
	if !w.Amount.IsPositive() {
		return apperror.NewValidation("amount must be positive").WithDetail("field", "amount")
	}
	if w.ApprovedBy != nil && !id.IsNil(w.CreatedBy) && *w.ApprovedBy == w.CreatedBy {
		return apperror.NewValidation("maker-checker violation: creator cannot approve treasury withdrawal")
	}
	return nil
}

// CanPost allows accounting movements only after the outbound workflow has
// broadcast the transaction and recorded its tx hash.
func (w *TreasuryWithdrawal) CanPost(ctx context.Context) error {
	if err := w.Document.CanPost(ctx); err != nil {
		return err
	}
	if w.Status != StatusBroadcast && w.Status != StatusConfirmed {
		return apperror.NewValidation("treasury withdrawal can be posted only after broadcast")
	}
	if w.TxHash == "" {
		return apperror.NewValidation("treasury withdrawal tx hash is required before posting")
	}
	return nil
}

func (w *TreasuryWithdrawal) GetRLSDimensions() map[string]string {
	return map[string]string{}
}

func (w *TreasuryWithdrawal) GetDocumentType() string { return DocumentType }

var _ posting.Postable = (*TreasuryWithdrawal)(nil)
