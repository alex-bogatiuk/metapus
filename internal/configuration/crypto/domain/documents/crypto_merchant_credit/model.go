// Package crypto_merchant_credit attributes an unidentified custody receipt to
// a merchant: suspense decreases by the gross amount, the merchant is credited
// net of the processing fee, and custody is untouched.
package crypto_merchant_credit

import (
	"context"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/registers/crypto_fee"
	"metapus/internal/configuration/crypto/domain/registers/crypto_merchant_balance"
	"metapus/internal/configuration/crypto/domain/registers/crypto_receipt_suspense"
	"metapus/internal/configuration/crypto/domain/tariff"
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

type CryptoMerchantCredit struct {
	entity.Document
	ReceiptID        id.ID           `db:"receipt_id" json:"receiptId" meta:"label:Поступление,ref:crypto_custody_receipt"`
	MerchantID       id.ID           `db:"merchant_id" json:"merchantId" meta:"label:Мерчант,ref:merchant"`
	InvoiceID        *id.ID          `db:"invoice_id" json:"invoiceId,omitempty" meta:"label:Счёт,ref:crypto_invoice"`
	TokenID          id.ID           `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`
	Amount           types.RawAmount `db:"amount" json:"amount" meta:"label:Сумма"`
	NetAmount        types.RawAmount `db:"net_amount" json:"netAmount" meta:"label:Зачислено мерчанту"`
	ProcessingFee    types.RawAmount `db:"processing_fee" json:"processingFee" meta:"label:Комиссия"`
	FeeFixed         types.RawAmount `db:"fee_fixed" json:"feeFixed" meta:"label:Фиксированная часть тарифа,list:false"`
	FeePercentBP     int             `db:"fee_percent_bp" json:"feePercentBp" meta:"label:Процент тарифа (б.п.),list:false"`
	FeeMin           types.RawAmount `db:"fee_min" json:"feeMin" meta:"label:Минимальная комиссия,list:false"`
	FeeMax           types.RawAmount `db:"fee_max" json:"feeMax" meta:"label:Максимальная комиссия,list:false"`
	Status           Status          `db:"status" json:"status" meta:"label:Статус"`
	ApprovalRequired bool            `db:"approval_required" json:"approvalRequired" meta:"label:Нужно подтверждение"`
	PreparedBy       id.ID           `db:"prepared_by" json:"preparedBy" meta:"label:Подготовил,list:false"`
	PreparedAt       time.Time       `db:"prepared_at" json:"preparedAt" meta:"label:Подготовлено"`
	DecidedBy        *id.ID          `db:"decided_by" json:"decidedBy,omitempty" meta:"label:Решение принял,list:false"`
	DecidedAt        *time.Time      `db:"decided_at" json:"decidedAt,omitempty" meta:"label:Решение принято"`
	DecisionReason   string          `db:"decision_reason" json:"decisionReason,omitempty" meta:"label:Причина отклонения,list:false"`
	SuspenseLineID   id.ID           `db:"suspense_line_id" json:"-"`
	MerchantLineID   id.ID           `db:"merchant_line_id" json:"-"`
	FeeLineID        id.ID           `db:"fee_line_id" json:"-"`
}

// Prepare creates a credit of the whole receipt remainder with the merchant
// tariff fixed at this moment.
func Prepare(receiptID, merchantID, tokenID id.ID, invoiceID *id.ID, amount types.RawAmount,
	fee tariff.Fee, approvalRequired bool, actor id.ID, at time.Time) (*CryptoMerchantCredit, error) {
	if id.IsNil(receiptID) || id.IsNil(merchantID) || id.IsNil(tokenID) || id.IsNil(actor) || !amount.IsPositive() ||
		(invoiceID != nil && id.IsNil(*invoiceID)) {
		return nil, apperror.NewValidation("merchant credit requires receipt, merchant, token, amount and operator")
	}
	processing, net := fee.Split(amount)
	if processing.Cmp(amount) > 0 {
		return nil, apperror.NewValidation("processing fee exceeds the received amount")
	}
	c := &CryptoMerchantCredit{
		Document: entity.NewDocument(), ReceiptID: receiptID, MerchantID: merchantID, InvoiceID: invoiceID,
		TokenID: tokenID, Amount: amount, ProcessingFee: processing, NetAmount: net,
		FeeFixed: fee.Fixed, FeePercentBP: fee.PercentBP, FeeMin: fee.Min, FeeMax: fee.Max,
		Status: StatusPrepared, ApprovalRequired: approvalRequired, PreparedBy: actor, PreparedAt: at.UTC(),
		SuspenseLineID: id.New(), MerchantLineID: id.New(), FeeLineID: id.New(),
	}
	c.Date = c.PreparedAt
	c.BasisType, c.BasisID = "CryptoCustodyReceipt", &c.ReceiptID
	return c, nil
}

// Approve records who posts the credit. A credit that needs approval cannot be
// posted by the operator who prepared it.
func (c *CryptoMerchantCredit) Approve(actor id.ID, at time.Time) error {
	if c.Status != StatusPrepared {
		return apperror.NewConflict("merchant credit is already decided")
	}
	if id.IsNil(actor) || (c.ApprovalRequired && actor == c.PreparedBy) {
		return apperror.NewForbidden("merchant credit requires approval by another operator")
	}
	decided := at.UTC()
	c.Status, c.DecidedBy, c.DecidedAt = StatusPosted, &actor, &decided
	return nil
}

func (c *CryptoMerchantCredit) Reject(actor id.ID, reason string, at time.Time) error {
	if c.Status != StatusPrepared {
		return apperror.NewConflict("merchant credit is already decided")
	}
	if id.IsNil(actor) || strings.TrimSpace(reason) == "" {
		return apperror.NewValidation("rejection requires operator and reason")
	}
	decided := at.UTC()
	c.Status, c.DecidedBy, c.DecidedAt, c.DecisionReason = StatusRejected, &actor, &decided, strings.TrimSpace(reason)
	return nil
}

func (c *CryptoMerchantCredit) GetDocumentType() string { return DocumentType }

// AttributedReceipt is the custody receipt whose remainder this document takes.
func (c *CryptoMerchantCredit) AttributedReceipt() id.ID { return c.ReceiptID }

func (c *CryptoMerchantCredit) Validate(ctx context.Context) error {
	if err := c.Document.Validate(ctx); err != nil {
		return err
	}
	if id.IsNil(c.ReceiptID) || id.IsNil(c.MerchantID) || id.IsNil(c.TokenID) || !c.Amount.IsPositive() ||
		!c.Amount.Equal(c.NetAmount.Add(c.ProcessingFee)) || c.Number == "" ||
		c.BasisID == nil || *c.BasisID != c.ReceiptID {
		return apperror.NewValidation("merchant credit requires a balanced attribution of its receipt")
	}
	return nil
}

func (c *CryptoMerchantCredit) CanPost(ctx context.Context) error {
	if err := c.Validate(ctx); err != nil {
		return err
	}
	if c.Posted || c.Status != StatusPosted || c.DecidedBy == nil {
		return apperror.NewConflict("merchant credit is posted once, after its decision")
	}
	return nil
}

func (*CryptoMerchantCredit) CanUnpost(context.Context) error {
	return apperror.NewConflict("merchant credit requires an explicit reversal document")
}

func (c *CryptoMerchantCredit) GenerateCryptoReceiptSuspenseMovements(context.Context) ([]crypto_receipt_suspense.CryptoReceiptSuspenseMovement, error) {
	m := crypto_receipt_suspense.NewCryptoReceiptSuspenseMovement(c.ID, DocumentType, 1, c.Date, entity.RecordTypeExpense, c.ReceiptID, c.TokenID, c.Amount)
	m.LineID = c.SuspenseLineID
	return []crypto_receipt_suspense.CryptoReceiptSuspenseMovement{m}, nil
}

func (c *CryptoMerchantCredit) GenerateCryptoMerchantBalanceMovements(context.Context) ([]crypto_merchant_balance.CryptoMerchantBalanceMovement, error) {
	if c.NetAmount.IsZero() {
		return nil, nil
	}
	m := crypto_merchant_balance.NewCryptoMerchantBalanceMovement(c.ID, DocumentType, 1, c.Date, entity.RecordTypeReceipt, c.MerchantID, c.TokenID, c.NetAmount)
	m.LineID = c.MerchantLineID
	return []crypto_merchant_balance.CryptoMerchantBalanceMovement{m}, nil
}

func (c *CryptoMerchantCredit) GenerateCryptoFeeMovements(context.Context) ([]crypto_fee.CryptoFeeMovement, error) {
	if c.ProcessingFee.IsZero() {
		return nil, nil
	}
	m := crypto_fee.NewCryptoFeeMovement(c.ID, DocumentType, 1, c.Date, entity.RecordTypeReceipt, c.MerchantID, c.TokenID, crypto_fee.FeeTypeProcessing, c.ProcessingFee)
	m.LineID = c.FeeLineID
	return []crypto_fee.CryptoFeeMovement{m}, nil
}

var _ posting.Postable = (*CryptoMerchantCredit)(nil)
var _ posting.UnpostGuard = (*CryptoMerchantCredit)(nil)
