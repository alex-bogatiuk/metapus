// Package treasury_withdrawal provides the TreasuryWithdrawal document.
// TreasuryWithdrawal records platform-owned crypto revenue withdrawals.
package treasury_withdrawal

import (
	"context"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/documents/outbound_settlement"
	"metapus/internal/configuration/crypto/domain/registers/crypto_fee"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
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

// Purpose says where the money of a treasury withdrawal comes from.
type Purpose string

const (
	// PurposeRevenue spends platform revenue to an allowlisted treasury address.
	PurposeRevenue Purpose = "revenue"
	// PurposeReceiptRefund returns an unidentified receipt to its sender: the
	// amount leaves receipt suspense into refund payable on approval and is paid
	// from a platform hot wallet to a destination recorded for this refund.
	PurposeReceiptRefund Purpose = "receipt_refund"
)

// GasPayer says who bears the network fee of a receipt refund.
type GasPayer string

const (
	// GasPayerPlatform records the network fee as a platform cost.
	GasPayerPlatform GasPayer = "platform"
	// GasPayerRecipient withholds a fixed amount in the refund token; the payer
	// receives the rest.
	GasPayerRecipient GasPayer = "recipient"
)

// MaxRefundTextBytes bounds the reason and the destination evidence of a refund.
const MaxRefundTextBytes = 2000

// TreasuryWithdrawal is an admin-only outbound payment from a platform hot
// wallet: a withdrawal of platform revenue, or the refund of an unidentified
// receipt to its sender.
type TreasuryWithdrawal struct {
	entity.Document

	Purpose        Purpose         `db:"purpose" json:"purpose" meta:"label:Назначение"`
	ReceiptID      *id.ID          `db:"receipt_id" json:"receiptId,omitempty" meta:"label:Возвращаемое поступление,ref:crypto_custody_receipt"`
	GasPayer       GasPayer        `db:"gas_payer" json:"gasPayer" meta:"label:Комиссию сети оплачивает"`
	WithheldAmount types.RawAmount `db:"withheld_amount" json:"withheldAmount" meta:"label:Удержано на комиссию"`

	TokenID                  id.ID                           `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`
	SourceWalletID           id.ID                           `db:"source_wallet_id" json:"sourceWalletId" meta:"label:Исходный кошелёк,ref:wallet"`
	DestAddress              string                          `db:"dest_address" json:"destAddress" meta:"label:Адрес назначения"`
	AddressID                id.ID                           `db:"address_id" json:"addressId" meta:"label:Адрес назначения,ref:treasury_withdrawal_address"`
	Amount                   types.RawAmount                 `db:"amount" json:"amount" meta:"label:Сумма"`
	NetworkFee               types.RawAmount                 `db:"network_fee" json:"networkFee" meta:"label:Комиссия сети"`
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
	NetFee                   types.RawAmount                 `db:"net_fee" json:"netFee"`
	EnergyFee                types.RawAmount                 `db:"energy_fee" json:"energyFee"`
	NetUsage                 int64                           `db:"net_usage" json:"netUsage"`

	FeeLines []FeeLine `db:"-" json:"feeLines,omitempty" meta:"label:Источники комиссии"`
}

// FeeLine identifies platform-revenue balance consumed by a treasury withdrawal.
type FeeLine struct {
	MerchantID id.ID              `json:"merchantId"`
	TokenID    id.ID              `json:"tokenId"`
	FeeType    crypto_fee.FeeType `json:"feeType"`
	Amount     types.RawAmount    `json:"amount"`
}

// NewTreasuryWithdrawal creates a pending admin treasury withdrawal.
func NewTreasuryWithdrawal(tokenID, sourceWalletID, addressID id.ID, destAddress string, amount types.RawAmount) *TreasuryWithdrawal {
	return &TreasuryWithdrawal{
		Document:         entity.NewDocument(),
		TokenID:          tokenID,
		SourceWalletID:   sourceWalletID,
		AddressID:        addressID,
		DestAddress:      destAddress,
		Amount:           amount,
		NetworkFee:       types.ZeroRawAmount(),
		Status:           StatusPendingApproval,
		SettlementStatus: outbound_settlement.StatusNotApplicable,
		Purpose:          PurposeRevenue,
		GasPayer:         GasPayerPlatform,
		WithheldAmount:   types.ZeroRawAmount(),
	}
}

// NewReceiptRefund returns total of an unidentified receipt to destinationID.
// With the recipient paying gas, withheld stays with the platform and the
// payer receives total-withheld; otherwise withheld must be zero.
func NewReceiptRefund(receiptID, tokenID, sourceWalletID, destinationID id.ID, destAddress string, total, withheld types.RawAmount, gasPayer GasPayer, reason string) (*TreasuryWithdrawal, error) {
	if !total.IsPositive() {
		return nil, apperror.NewValidation("refund amount must be positive").WithDetail("field", "amount")
	}
	if withheld.Cmp(total) >= 0 {
		return nil, apperror.NewValidation("withheld network fee must be below the refund amount").WithDetail("field", "withheldAmount")
	}
	doc := NewTreasuryWithdrawal(tokenID, sourceWalletID, destinationID, destAddress, total.Sub(withheld))
	doc.Purpose = PurposeReceiptRefund
	doc.ReceiptID = &receiptID
	doc.GasPayer = gasPayer
	doc.WithheldAmount = withheld
	doc.Description = strings.TrimSpace(reason)
	return doc, doc.validateRefundTerms()
}

// IsReceiptRefund reports whether the withdrawal returns an unidentified receipt.
func (w *TreasuryWithdrawal) IsReceiptRefund() bool { return w.Purpose == PurposeReceiptRefund }

// RefundTotal is what a receipt refund takes out of receipt suspense.
func (w *TreasuryWithdrawal) RefundTotal() types.RawAmount { return w.Amount.Add(w.WithheldAmount) }

func (w *TreasuryWithdrawal) validateRefundTerms() error {
	switch w.Purpose {
	case PurposeRevenue, "":
		if w.ReceiptID != nil || w.GasPayer == GasPayerRecipient || w.WithheldAmount.IsPositive() {
			return apperror.NewValidation("a revenue withdrawal carries no refund terms")
		}
		return nil
	case PurposeReceiptRefund:
	default:
		return apperror.NewValidation("unknown treasury withdrawal purpose").WithDetail("field", "purpose")
	}
	if w.ReceiptID == nil || id.IsNil(*w.ReceiptID) {
		return apperror.NewValidation("receipt refund requires its receipt").WithDetail("field", "receiptId")
	}
	switch w.GasPayer {
	case GasPayerPlatform:
		if !w.WithheldAmount.IsZero() {
			return apperror.NewValidation("platform-paid gas withholds nothing").WithDetail("field", "withheldAmount")
		}
	case GasPayerRecipient:
		if !w.WithheldAmount.IsPositive() {
			return apperror.NewValidation("recipient-paid gas requires the withheld amount").WithDetail("field", "withheldAmount")
		}
	default:
		return apperror.NewValidation("gas payer must be platform or recipient").WithDetail("field", "gasPayer")
	}
	if w.Description == "" || len(w.Description) > MaxRefundTextBytes {
		return apperror.NewValidation("refund reason is required and at most 2000 bytes").WithDetail("field", "reason")
	}
	return nil
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
	return w.validateRefundTerms()
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
