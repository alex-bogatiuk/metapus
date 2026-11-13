// Package crypto_platform_funding takes an unidentified custody receipt as
// platform money: platform funding that arrived without a prior funding
// record. Suspense decreases by the whole remainder; custody already holds the
// transfer and no merchant or fee line moves. Another operator decides.
package crypto_platform_funding

import (
	"context"
	"strings"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

type Status string

const (
	StatusPrepared Status = "prepared"
	StatusPosted   Status = "posted"
	StatusRejected Status = "rejected"
)

// MerchantLink says why a receipt most likely belongs to a merchant.
type MerchantLink string

const (
	LinkCustodyOwner      MerchantLink = "custody_owner"      // merchant-owned wallet or dedicated pool
	LinkDepositAddress    MerchantLink = "deposit_address"    // persistent deposit address of a merchant customer
	LinkInvoiceAllocation MerchantLink = "invoice_allocation" // wallet leased to a merchant invoice at arrival
)

// MerchantTie is the merchant a receipt is tied to through its wallet.
type MerchantTie struct {
	MerchantID   id.ID
	MerchantName string
	Link         MerchantLink
}

const maxReasonBytes = 2000

type CryptoPlatformFunding struct {
	entity.Document
	ReceiptID                id.ID              `db:"receipt_id" json:"receiptId" meta:"label:Поступление,ref:crypto_custody_receipt"`
	WalletID                 id.ID              `db:"wallet_id" json:"walletId" meta:"label:Кошелёк,ref:wallet"`
	TokenID                  id.ID              `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`
	Amount                   types.CryptoAmount `db:"amount" json:"amount" meta:"label:Сумма"`
	Reason                   string             `db:"reason" json:"reason" meta:"label:Основание"`
	MerchantID               *id.ID             `db:"merchant_id" json:"merchantId,omitempty" meta:"label:Связанный мерчант,ref:merchant"`
	MerchantLink             *MerchantLink      `db:"merchant_link" json:"merchantLink,omitempty" meta:"label:Связь с мерчантом"`
	MerchantLinkAcknowledged bool               `db:"merchant_link_acknowledged" json:"merchantLinkAcknowledged" meta:"label:Связь подтверждена,list:false"`
	Status                   Status             `db:"status" json:"status" meta:"label:Статус"`
	PreparedBy               id.ID              `db:"prepared_by" json:"preparedBy" meta:"label:Подготовил,list:false"`
	PreparedAt               time.Time          `db:"prepared_at" json:"preparedAt" meta:"label:Подготовлено"`
	DecidedBy                *id.ID             `db:"decided_by" json:"decidedBy,omitempty" meta:"label:Подтвердил,list:false"`
	DecidedAt                *time.Time         `db:"decided_at" json:"decidedAt,omitempty" meta:"label:Решение принято"`
	DecisionReason           string             `db:"decision_reason" json:"decisionReason,omitempty" meta:"label:Причина отклонения,list:false"`
	SuspenseLineID           id.ID              `db:"suspense_line_id" json:"-"`
}

// ErrMerchantTieUnacknowledged refuses a receipt tied to a merchant until the
// operator confirms that it is platform money nevertheless.
func ErrMerchantTieUnacknowledged(tie MerchantTie) error {
	return apperror.NewValidation("receipt is tied to a merchant; confirm that it is platform money").
		WithDetail("merchantId", tie.MerchantID.String()).WithDetail("merchantName", tie.MerchantName).
		WithDetail("merchantLink", string(tie.Link))
}

// Prepare takes the whole receipt remainder. A receipt tied to a merchant needs
// the operator's acknowledgement of that merchant.
func Prepare(receiptID, walletID, tokenID id.ID, amount types.CryptoAmount, reason string, tie *MerchantTie,
	acknowledged bool, actor id.ID, at time.Time) (*CryptoPlatformFunding, error) {
	reason = strings.TrimSpace(reason)
	if id.IsNil(receiptID) || id.IsNil(walletID) || id.IsNil(tokenID) || id.IsNil(actor) || !amount.IsPositive() {
		return nil, apperror.NewValidation("platform funding requires receipt, wallet, token, amount and operator")
	}
	if reason == "" || len(reason) > maxReasonBytes {
		return nil, apperror.NewValidation("platform funding requires a reason (up to 2000 bytes)")
	}
	f := &CryptoPlatformFunding{
		Document: entity.NewDocument(), ReceiptID: receiptID, WalletID: walletID, TokenID: tokenID, Amount: amount,
		Reason: reason, Status: StatusPrepared, PreparedBy: actor, PreparedAt: at.UTC(), SuspenseLineID: id.New(),
	}
	if tie != nil {
		if !acknowledged {
			return nil, ErrMerchantTieUnacknowledged(*tie)
		}
		merchant, link := tie.MerchantID, tie.Link
		f.MerchantID, f.MerchantLink, f.MerchantLinkAcknowledged = &merchant, &link, true
	}
	f.Date = f.PreparedAt
	f.BasisType, f.BasisID = "CryptoCustodyReceipt", &f.ReceiptID
	return f, nil
}

// Approve posts the funding. Platform money leaves suspense without a third
// party claim, so the operator who prepared it can never approve it.
func (f *CryptoPlatformFunding) Approve(actor id.ID, at time.Time) error {
	if f.Status != StatusPrepared {
		return apperror.NewConflict("platform funding is already decided")
	}
	if id.IsNil(actor) || actor == f.PreparedBy {
		return apperror.NewForbidden("platform funding requires approval by another operator")
	}
	decided := at.UTC()
	f.Status, f.DecidedBy, f.DecidedAt = StatusPosted, &actor, &decided
	return nil
}

func (f *CryptoPlatformFunding) Reject(actor id.ID, reason string, at time.Time) error {
	if f.Status != StatusPrepared {
		return apperror.NewConflict("platform funding is already decided")
	}
	reason = strings.TrimSpace(reason)
	if id.IsNil(actor) || reason == "" || len(reason) > maxReasonBytes {
		return apperror.NewValidation("rejection requires operator and reason (up to 2000 bytes)")
	}
	decided := at.UTC()
	f.Status, f.DecidedBy, f.DecidedAt, f.DecisionReason = StatusRejected, &actor, &decided, reason
	return nil
}

// TieMatches reports whether the receipt is still tied exactly as recorded.
func (f *CryptoPlatformFunding) TieMatches(tie *MerchantTie) bool {
	if tie == nil || f.MerchantID == nil || f.MerchantLink == nil {
		return tie == nil && f.MerchantID == nil
	}
	return tie.MerchantID == *f.MerchantID && tie.Link == *f.MerchantLink
}

func (f *CryptoPlatformFunding) GetDocumentType() string { return DocumentType }

func (f *CryptoPlatformFunding) Validate(ctx context.Context) error {
	if err := f.Document.Validate(ctx); err != nil {
		return err
	}
	if id.IsNil(f.ReceiptID) || id.IsNil(f.WalletID) || id.IsNil(f.TokenID) || !f.Amount.IsPositive() ||
		strings.TrimSpace(f.Reason) == "" || f.Number == "" || f.BasisID == nil || *f.BasisID != f.ReceiptID ||
		(f.MerchantID == nil) != (f.MerchantLink == nil) || (f.MerchantID != nil && !f.MerchantLinkAcknowledged) {
		return apperror.NewValidation("platform funding requires its receipt, reason and acknowledged merchant tie")
	}
	return nil
}

func (f *CryptoPlatformFunding) CanPost(ctx context.Context) error {
	if err := f.Validate(ctx); err != nil {
		return err
	}
	if f.Posted || f.Status != StatusPosted || f.DecidedBy == nil {
		return apperror.NewConflict("platform funding is posted once, after its decision")
	}
	return nil
}

func (*CryptoPlatformFunding) CanUnpost(context.Context) error {
	return apperror.NewConflict("platform funding is reversed only by the orphaning of its receipt")
}

func (f *CryptoPlatformFunding) GenerateCryptoReceiptSuspenseMovements(context.Context) ([]entity.CryptoReceiptSuspenseMovement, error) {
	m := entity.NewCryptoReceiptSuspenseMovement(f.ID, DocumentType, 1, f.Date, entity.RecordTypeExpense, f.ReceiptID, f.TokenID, f.Amount)
	m.LineID = f.SuspenseLineID
	return []entity.CryptoReceiptSuspenseMovement{m}, nil
}

var _ posting.Postable = (*CryptoPlatformFunding)(nil)
var _ posting.UnpostGuard = (*CryptoPlatformFunding)(nil)
