// Package withdrawal_request provides the domain model for merchant withdrawal requests.
// WithdrawalRequest is a Postable document that implements the debit-first pattern:
// upon creation, it immediately generates an Expense movement against the merchant balance,
// locking the funds. If rejected, a storno (compensating receipt) movement is created
// to restore the balance. An unpost appends linked reversal movements.
package withdrawal_request

import (
	"context"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/registers/crypto_merchant_balance"
	"metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/posting"
	"metapus/pkg/sdk/types"
)

// Status represents the lifecycle state of a withdrawal request.
type Status string

const DocumentType = "WithdrawalRequest"

const (
	StatusPendingSignature   Status = "pending_signature"
	StatusPendingApproval    Status = "pending_approval"
	StatusApproved           Status = "approved"
	StatusSigning            Status = "signing"
	StatusBroadcast          Status = "broadcast"
	StatusConfirmed          Status = "confirmed"
	StatusHeld               Status = "held"
	StatusRejected           Status = "rejected"
	StatusFailed             Status = "failed"
	StatusPartiallyCompleted Status = "partially_completed"
)

// IsTerminal returns true if the status is final (no further transitions).
func (s Status) IsTerminal() bool {
	return s == StatusConfirmed || s == StatusRejected || s == StatusFailed || s == StatusPartiallyCompleted
}

// NeedsBalanceRestore returns true if entering this status requires
// creating storno movements to restore the merchant balance.
func (s Status) NeedsBalanceRestore() bool {
	return s == StatusRejected || s == StatusFailed
}

// WithdrawalRequest is a Postable document that reserves merchant balance upon creation.
// Implements posting.Postable + cryptoposting.CryptoMerchantBalanceMovementSource.
//
// Lifecycle:
//   - Created → Engine.Post() → Expense movement → balance reduced
//   - Approved / Signing / Broadcast → no balance change
//   - Confirmed → balance stays reduced (withdrawal fulfilled)
//   - Rejected / Failed → StornoMovements() → compensating receipt → balance restored
//   - Unposted → Engine.Unpost() → linked compensating movements
type WithdrawalRequest struct {
	entity.Document

	// MerchantID — who is requesting the withdrawal
	MerchantID id.ID `db:"merchant_id" json:"merchantId" meta:"label:Мерчант,ref:merchant"`

	// TokenID identifies the token being withdrawn
	TokenID id.ID `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`

	// Amount to withdraw (token minor units)
	Amount types.RawAmount `db:"amount" json:"amount" meta:"label:Сумма"`

	// DestAddress — external blockchain address
	DestAddress string `db:"dest_address" json:"destAddress" meta:"label:Адрес назначения"`

	// AddressID — reference to the whitelisted address
	AddressID id.ID `db:"address_id" json:"addressId" meta:"label:Адрес из белого списка"`

	// Status is the current lifecycle state
	Status Status `db:"status" json:"status" meta:"label:Статус"`

	// ApprovedBy — who approved the request (nil until approved)
	ApprovedBy *id.ID `db:"approved_by" json:"approvedBy,omitempty" meta:"label:Утвердил,ref:user"`

	// ApprovedAt — when the request was approved
	ApprovedAt *time.Time `db:"approved_at" json:"approvedAt,omitempty" meta:"label:Утверждено"`

	// RejectionReason — filled on rejection/failure
	RejectionReason string `db:"rejection_reason" json:"rejectionReason,omitempty" meta:"label:Причина отклонения"`

	// WithdrawalID — link to the actual CryptoWithdrawal doc (filled after approval)
	WithdrawalID *id.ID `db:"withdrawal_id" json:"withdrawalId,omitempty" meta:"label:Крипто-вывод,ref:crypto_withdrawal"`
}

// RefundBasisType is the basis of a merchant refund: the credited CryptoPayment
// the request returns to its payer. A refund is an ordinary debit-first
// withdrawal; the basis only ties it to the payment and caps it by that payment.
const RefundBasisType = "CryptoPayment"

// MaxRefundReasonBytes bounds the reason a refund carries in its description.
const MaxRefundReasonBytes = 500

// MarkAsRefund ties the request to the payment it returns and records why.
func (w *WithdrawalRequest) MarkAsRefund(paymentID id.ID, reason string) {
	w.BasisType = RefundBasisType
	w.BasisID = &paymentID
	w.Description = strings.TrimSpace(reason)
}

// IsRefund reports whether the request returns a credited payment.
func (w *WithdrawalRequest) IsRefund() bool { return w.BasisType == RefundBasisType }

// NewWithdrawalRequest creates a new WithdrawalRequest in pending_approval state.
// The document should be posted immediately after creation via Engine.Post().
func NewWithdrawalRequest(
	merchantID, tokenID, addressID id.ID,
	destAddress string,
	amount types.RawAmount,
) *WithdrawalRequest {
	return &WithdrawalRequest{
		Document:    entity.NewDocument(),
		MerchantID:  merchantID,
		TokenID:     tokenID,
		Amount:      amount,
		DestAddress: destAddress,
		AddressID:   addressID,
		Status:      StatusPendingApproval,
	}
}

// --- Validatable ---

// Validate implements entity.Validatable — pure function, no DB calls.
func (w *WithdrawalRequest) Validate(ctx context.Context) error {
	if err := w.Document.Validate(ctx); err != nil {
		return err
	}

	if id.IsNil(w.MerchantID) {
		return apperror.NewValidation("merchant is required").WithDetail("field", "merchantId")
	}
	if id.IsNil(w.TokenID) {
		return apperror.NewValidation("token is required").WithDetail("field", "tokenId")
	}
	if !w.Amount.IsPositive() {
		return apperror.NewValidation("amount must be positive").WithDetail("field", "amount")
	}
	if w.DestAddress == "" {
		return apperror.NewValidation("destination address is required").WithDetail("field", "destAddress")
	}
	if id.IsNil(w.AddressID) {
		return apperror.NewValidation("address ID is required").WithDetail("field", "addressId")
	}
	switch w.BasisType {
	case "":
		if w.BasisID != nil {
			return apperror.NewValidation("withdrawal basis type is required").WithDetail("field", "basisType")
		}
	case RefundBasisType:
		if w.BasisID == nil || id.IsNil(*w.BasisID) {
			return apperror.NewValidation("refund requires the payment it returns").WithDetail("field", "refundPaymentId")
		}
		if strings.TrimSpace(w.Description) == "" {
			return apperror.NewValidation("refund reason is required").WithDetail("field", "reason")
		}
		if len(w.Description) > MaxRefundReasonBytes {
			return apperror.NewValidation("refund reason exceeds 500 bytes").WithDetail("field", "reason")
		}
	default:
		return apperror.NewValidation("withdrawal basis must be a credited payment").WithDetail("field", "basisType")
	}
	return nil
}

// --- Postable interface ---

// GetDocumentType returns the document type name for posting engine.
func (w *WithdrawalRequest) GetDocumentType() string { return DocumentType }

// --- CryptoMerchantBalanceMovementSource ---

// GenerateCryptoMerchantBalanceMovements creates an EXPENSE movement
// that immediately reduces the merchant's available balance.
// This is the core of the debit-first pattern.
func (w *WithdrawalRequest) GenerateCryptoMerchantBalanceMovements(ctx context.Context) ([]crypto_merchant_balance.CryptoMerchantBalanceMovement, error) {
	if w.Amount.IsZero() {
		return nil, nil
	}

	newVersion := w.PostedVersion + 1

	movement := crypto_merchant_balance.NewCryptoMerchantBalanceMovement(
		w.ID,
		w.GetDocumentType(),
		newVersion,
		w.Date,
		entity.RecordTypeExpense,
		w.MerchantID,
		w.TokenID,
		w.Amount,
	)

	return []crypto_merchant_balance.CryptoMerchantBalanceMovement{movement}, nil
}

// --- RLSDimensionable override ---

// GetRLSDimensions returns merchant dimension for RLS filtering.
func (w *WithdrawalRequest) GetRLSDimensions() map[string]string {
	return map[string]string{
		"merchant": w.MerchantID.String(),
	}
}

// Compile-time interface checks.
var _ posting.Postable = (*WithdrawalRequest)(nil)
var _ cryptoposting.CryptoMerchantBalanceMovementSource = (*WithdrawalRequest)(nil)
