package crypto_custody_receipt

import (
	"context"

	"metapus/internal/configuration/crypto/domain/registers/crypto_balance"
	"metapus/internal/configuration/crypto/domain/registers/crypto_receipt_suspense"
	"metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

type Reason string

const (
	ReasonNoAllocation Reason = "no_allocation"
	ReasonWrongToken   Reason = "wrong_token"
	ReasonBeforeLease  Reason = "timestamp_before_lease"
	ReasonRetired      Reason = "retired_deposit_address"
	ReasonAdditional   Reason = "additional_single_payment"
	ReasonOverlapping  Reason = "overlapping_windows"
	ReasonDust         Reason = "dust"
	ReasonTreasury     Reason = "treasury_unidentified"
	ReasonMatched      Reason = "matched_receipt"
)

func (r Reason) Valid() bool {
	switch r {
	case ReasonNoAllocation, ReasonWrongToken, ReasonBeforeLease, ReasonRetired,
		ReasonAdditional, ReasonOverlapping, ReasonDust, ReasonTreasury, ReasonMatched:
		return true
	default:
		return false
	}
}

// CryptoCustodyReceipt records custody and unallocated economic purpose
// independently of invoice attribution, merchant credit or risk classification.
type CryptoCustodyReceipt struct {
	entity.Document
	NetworkID         id.ID           `db:"network_id" json:"networkId" meta:"label:Сеть,ref:blockchain_network"`
	WalletID          id.ID           `db:"wallet_id" json:"walletId" meta:"label:Кошелёк,ref:wallet"`
	TokenID           id.ID           `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`
	CustodyMerchantID *id.ID          `db:"custody_merchant_id" json:"custodyMerchantId,omitempty" meta:"label:Владелец custody,ref:merchant"`
	VerificationID    id.ID           `db:"verification_id" json:"verificationId" meta:"list:false"`
	OccurrenceID      id.ID           `db:"occurrence_id" json:"occurrenceId" meta:"list:false"`
	PostingID         id.ID           `db:"posting_id" json:"postingId" meta:"list:false"`
	Amount            types.RawAmount `db:"amount" json:"amount" meta:"label:Сумма"`
	CustodyLineID     id.ID           `db:"custody_line_id" json:"custodyLineId" meta:"list:false"`
	SuspenseLineID    id.ID           `db:"suspense_line_id" json:"suspenseLineId" meta:"list:false"`
	ReasonCode        Reason          `db:"reason_code" json:"reasonCode" meta:"label:Причина"`
}

func New() *CryptoCustodyReceipt {
	return &CryptoCustodyReceipt{
		Document: entity.NewDocument(), CustodyLineID: id.New(), SuspenseLineID: id.New(),
	}
}

func (r *CryptoCustodyReceipt) GetDocumentType() string { return DocumentType }

func (r *CryptoCustodyReceipt) Validate(ctx context.Context) error {
	if err := r.Document.Validate(ctx); err != nil {
		return err
	}
	if id.IsNil(r.ID) || id.IsNil(r.NetworkID) || id.IsNil(r.WalletID) || id.IsNil(r.TokenID) ||
		id.IsNil(r.VerificationID) || id.IsNil(r.OccurrenceID) || id.IsNil(r.PostingID) ||
		id.IsNil(r.CustodyLineID) || id.IsNil(r.SuspenseLineID) || r.CustodyLineID == r.SuspenseLineID ||
		!r.Amount.IsPositive() || !r.ReasonCode.Valid() || r.Number == "" ||
		r.BasisType != "CryptoTransferVerification" || r.BasisID == nil || *r.BasisID != r.VerificationID {
		return apperror.NewValidation("custody receipt requires complete immutable evidence")
	}
	if r.CustodyMerchantID != nil && id.IsNil(*r.CustodyMerchantID) {
		return apperror.NewValidation("invalid custody owner")
	}
	return nil
}

func (r *CryptoCustodyReceipt) CanPost(ctx context.Context) error {
	if err := r.Validate(ctx); err != nil {
		return err
	}
	if r.Posted || r.PostedVersion != 0 || r.DeletionMark {
		return apperror.NewConflict("custody receipt cannot be reposted")
	}
	return nil
}

func (*CryptoCustodyReceipt) CanModify() error {
	return apperror.NewForbidden("custody receipt is a system-owned immutable fact")
}

func (*CryptoCustodyReceipt) CanUnpost(context.Context) error {
	return apperror.NewConflict("custody receipt requires explicit compensation")
}

func (r *CryptoCustodyReceipt) GenerateCryptoBalanceMovements(context.Context) ([]crypto_balance.CryptoBalanceMovement, error) {
	movement := crypto_balance.NewCryptoBalanceMovement(r.ID, DocumentType, r.PostedVersion+1,
		r.Date, entity.RecordTypeReceipt, r.WalletID, r.TokenID, r.Amount)
	movement.LineID = r.CustodyLineID
	return []crypto_balance.CryptoBalanceMovement{movement}, nil
}

func (r *CryptoCustodyReceipt) GenerateCryptoReceiptSuspenseMovements(context.Context) ([]crypto_receipt_suspense.CryptoReceiptSuspenseMovement, error) {
	movement := crypto_receipt_suspense.NewCryptoReceiptSuspenseMovement(r.ID, DocumentType, r.PostedVersion+1,
		r.Date, entity.RecordTypeReceipt, r.ID, r.TokenID, r.Amount)
	movement.LineID = r.SuspenseLineID
	return []crypto_receipt_suspense.CryptoReceiptSuspenseMovement{movement}, nil
}

var _ posting.Postable = (*CryptoCustodyReceipt)(nil)
var _ posting.UnpostGuard = (*CryptoCustodyReceipt)(nil)
var _ cryptoposting.CryptoBalanceMovementSource = (*CryptoCustodyReceipt)(nil)
var _ cryptoposting.CryptoReceiptSuspenseMovementSource = (*CryptoCustodyReceipt)(nil)
