// Package withdrawal_request provides the domain model for merchant withdrawal requests.
// WithdrawalRequest is a Postable document that implements the debit-first pattern:
// upon creation, it immediately generates an Expense movement against the merchant balance,
// locking the funds. If rejected, a storno (compensating receipt) movement is created
// to restore the balance. If the document is unposted, Engine.Unpost() deletes all movements.
package withdrawal_request

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

// Status represents the lifecycle state of a withdrawal request.
type Status string

const (
	StatusPendingApproval Status = "pending_approval"
	StatusApproved        Status = "approved"
	StatusSigning         Status = "signing"
	StatusBroadcast       Status = "broadcast"
	StatusConfirmed       Status = "confirmed"
	StatusRejected        Status = "rejected"
	StatusFailed          Status = "failed"
)

// IsTerminal returns true if the status is final (no further transitions).
func (s Status) IsTerminal() bool {
	return s == StatusConfirmed || s == StatusRejected || s == StatusFailed
}

// NeedsBalanceRestore returns true if entering this status requires
// creating storno movements to restore the merchant balance.
func (s Status) NeedsBalanceRestore() bool {
	return s == StatusRejected || s == StatusFailed
}

// WithdrawalRequest is a Postable document that reserves merchant balance upon creation.
// Implements posting.Postable + posting.CryptoMerchantBalanceMovementSource.
//
// Lifecycle:
//   - Created → Engine.Post() → Expense movement → balance reduced
//   - Approved / Signing / Broadcast → no balance change
//   - Confirmed → balance stays reduced (withdrawal fulfilled)
//   - Rejected / Failed → StornoMovements() → compensating receipt → balance restored
//   - Unposted → Engine.Unpost() → DELETE all movements (standard 1C behavior)
type WithdrawalRequest struct {
	entity.Document

	// MerchantID — who is requesting the withdrawal
	MerchantID id.ID `db:"merchant_id" json:"merchantId"`

	// TokenID identifies the token being withdrawn
	TokenID id.ID `db:"token_id" json:"tokenId"`

	// Amount to withdraw (token minor units)
	Amount types.CryptoAmount `db:"amount" json:"amount"`

	// DestAddress — external blockchain address
	DestAddress string `db:"dest_address" json:"destAddress"`

	// AddressID — reference to the whitelisted address
	AddressID id.ID `db:"address_id" json:"addressId"`

	// Status is the current lifecycle state
	Status Status `db:"status" json:"status"`

	// ApprovedBy — who approved the request (nil until approved)
	ApprovedBy *id.ID `db:"approved_by" json:"approvedBy,omitempty"`

	// ApprovedAt — when the request was approved
	ApprovedAt *time.Time `db:"approved_at" json:"approvedAt,omitempty"`

	// RejectionReason — filled on rejection/failure
	RejectionReason string `db:"rejection_reason" json:"rejectionReason,omitempty"`

	// WithdrawalID — link to the actual CryptoWithdrawal doc (filled after approval)
	WithdrawalID *id.ID `db:"withdrawal_id" json:"withdrawalId,omitempty"`
}

// NewWithdrawalRequest creates a new WithdrawalRequest in pending_approval state.
// The document should be posted immediately after creation via Engine.Post().
func NewWithdrawalRequest(
	merchantID, tokenID, addressID id.ID,
	destAddress string,
	amount types.CryptoAmount,
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
	return nil
}

// --- Postable interface ---

// GetDocumentType returns the document type name for posting engine.
func (w *WithdrawalRequest) GetDocumentType() string { return "WithdrawalRequest" }

// --- CryptoMerchantBalanceMovementSource ---

// GenerateCryptoMerchantBalanceMovements creates an EXPENSE movement
// that immediately reduces the merchant's available balance.
// This is the core of the debit-first pattern.
func (w *WithdrawalRequest) GenerateCryptoMerchantBalanceMovements(ctx context.Context) ([]entity.CryptoMerchantBalanceMovement, error) {
	if w.Amount.IsZero() {
		return nil, nil
	}

	newVersion := w.PostedVersion + 1

	movement := entity.NewCryptoMerchantBalanceMovement(
		w.ID,
		w.GetDocumentType(),
		newVersion,
		w.Date,
		entity.RecordTypeExpense,
		w.MerchantID,
		w.TokenID,
		w.Amount,
	)

	return []entity.CryptoMerchantBalanceMovement{movement}, nil
}

// --- RLSDimensionable override ---

// GetRLSDimensions returns merchant dimension for RLS filtering.
func (w *WithdrawalRequest) GetRLSDimensions() map[string]string {
	return map[string]string{
		"merchant": w.MerchantID.String(),
	}
}

// --- CurrencyAwareDoc stubs (not applicable for crypto docs) ---

func (w *WithdrawalRequest) GetCurrencyID() id.ID                    { return id.ID{} }
func (w *WithdrawalRequest) SetCurrencyID(_ id.ID)                    {}
func (w *WithdrawalRequest) ValidateCurrency(_ context.Context) error { return nil }
func (w *WithdrawalRequest) GetContractID() *id.ID                    { return nil }

// Compile-time interface checks.
var _ posting.Postable = (*WithdrawalRequest)(nil)
var _ posting.CryptoMerchantBalanceMovementSource = (*WithdrawalRequest)(nil)
