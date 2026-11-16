package cryptoapp

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_merchant_credit"
)

// ReceiptRemainder is the unattributed part of a custody receipt, read under
// the receipt's suspense balance lock.
type ReceiptRemainder struct {
	ReceiptID         id.ID
	WalletID          id.ID
	TokenID           id.ID
	Amount            types.RawAmount
	CustodyMerchantID *id.ID
	// Orphaned receipts stay on the books for investigation but cannot be attributed.
	Orphaned bool
	// PaymentReorged: the invoice payment of this receipt was compensated by a
	// chain reorganization. Its re-included transfer attributes the receipt
	// again, and the receipt audit orphans it if the transfer stays absent, so
	// an operator credit would race the payment for the same money.
	PaymentReorged bool
	// AwaitingDecision: a prepared merchant credit, platform funding or receipt refund already
	// claims the remainder until another operator decides.
	AwaitingDecision bool
}

var (
	errReceiptOrphaned       = apperror.NewConflict("receipt transfer was orphaned by a chain reorganization; resolve its incident first")
	errReceiptPaymentReorged = apperror.NewConflict("invoice payment of this receipt was reversed by a chain reorganization; wait for its re-inclusion or the receipt audit")
	errReceiptAwaitsDecision = apperror.NewConflict("receipt already awaits a decision on another attribution")
)

// attributable rejects a remainder that only the chain may decide about.
func (r ReceiptRemainder) attributable() error {
	if r.Orphaned {
		return errReceiptOrphaned
	}
	if r.PaymentReorged {
		return errReceiptPaymentReorged
	}
	return nil
}

type MerchantCreditRepository interface {
	LockReceiptRemainder(context.Context, id.ID) (ReceiptRemainder, error)
	ApprovalRequired(context.Context, id.ID, types.RawAmount) (bool, error)
	Create(context.Context, *crypto_merchant_credit.CryptoMerchantCredit) error
	GetByID(context.Context, id.ID) (*crypto_merchant_credit.CryptoMerchantCredit, error)
	GetByIDForUpdate(context.Context, id.ID) (*crypto_merchant_credit.CryptoMerchantCredit, error)
	Update(context.Context, *crypto_merchant_credit.CryptoMerchantCredit) error
}

type MerchantFeeResolver interface {
	Resolve(context.Context, id.ID, id.ID, crypto.FeeDirection) (crypto.EffectiveFee, error)
}

type PrepareMerchantCreditCommand struct {
	ReceiptID, MerchantID id.ID
	InvoiceID             *id.ID
	Actor                 id.ID
}

type DecideMerchantCreditCommand struct {
	CreditID        id.ID
	ExpectedVersion int
	Actor           id.ID
	Reason          string
}

// MerchantCreditService attributes unidentified receipts to merchants. The
// whole remainder is credited at the merchant tariff of the preparation time.
type MerchantCreditService struct {
	txm     tx.Manager
	repo    MerchantCreditRepository
	fees    MerchantFeeResolver
	posting FinancialPostingEngine
	numbers numerator.Generator
	outbox  domain.OutboxPublisher
	now     func() time.Time
}

func NewMerchantCreditService(txm tx.Manager, repo MerchantCreditRepository, fees MerchantFeeResolver,
	posting FinancialPostingEngine, numbers numerator.Generator, outbox domain.OutboxPublisher, now func() time.Time) *MerchantCreditService {
	if txm == nil || repo == nil || fees == nil || posting == nil || numbers == nil || outbox == nil || now == nil {
		panic("merchant credit requires transaction, repository, tariff, posting, numbering and outbox")
	}
	return &MerchantCreditService{txm: txm, repo: repo, fees: fees, posting: posting, numbers: numbers, outbox: outbox, now: now}
}

func (s *MerchantCreditService) Prepare(ctx context.Context, cmd PrepareMerchantCreditCommand) (*crypto_merchant_credit.CryptoMerchantCredit, error) {
	var result *crypto_merchant_credit.CryptoMerchantCredit
	err := s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		remainder, err := s.repo.LockReceiptRemainder(ctx, cmd.ReceiptID)
		if err != nil {
			return err
		}
		if err := remainder.attributable(); err != nil {
			return err
		}
		if remainder.AwaitingDecision {
			return errReceiptAwaitsDecision
		}
		if !remainder.Amount.IsPositive() {
			return apperror.NewConflict("receipt has no unattributed remainder")
		}
		if remainder.CustodyMerchantID != nil && *remainder.CustodyMerchantID != cmd.MerchantID {
			return apperror.NewForbidden("receipt on a merchant-owned wallet can be credited only to its owner")
		}
		tariff, err := s.fees.Resolve(ctx, cmd.MerchantID, remainder.TokenID, crypto.FeeDirectionProcessing)
		if err != nil {
			return fmt.Errorf("resolve merchant tariff: %w", err)
		}
		approval, err := s.repo.ApprovalRequired(ctx, remainder.TokenID, remainder.Amount)
		if err != nil {
			return err
		}
		doc, err := crypto_merchant_credit.Prepare(cmd.ReceiptID, cmd.MerchantID, remainder.TokenID, cmd.InvoiceID, remainder.Amount,
			tariff.Tariff(),
			approval, cmd.Actor, s.now())
		if err != nil {
			return err
		}
		if doc.Number, err = s.numbers.GetNextNumber(ctx, numerator.DefaultConfig("CMC"), nil, doc.Date); err != nil {
			return fmt.Errorf("number merchant credit: %w", err)
		}
		if err := s.repo.Create(ctx, doc); err != nil {
			return err
		}
		if !approval {
			if err := s.post(ctx, doc, cmd.Actor); err != nil {
				return err
			}
		} else if err := s.publish(ctx, doc, "prepared"); err != nil {
			return err
		}
		result = doc
		return nil
	})
	return result, err
}

func (s *MerchantCreditService) Approve(ctx context.Context, cmd DecideMerchantCreditCommand) (*crypto_merchant_credit.CryptoMerchantCredit, error) {
	return s.decide(ctx, cmd, func(ctx context.Context, doc *crypto_merchant_credit.CryptoMerchantCredit, remainder ReceiptRemainder) error {
		if err := remainder.attributable(); err != nil {
			return err
		}
		return s.post(ctx, doc, cmd.Actor)
	})
}

func (s *MerchantCreditService) Reject(ctx context.Context, cmd DecideMerchantCreditCommand) (*crypto_merchant_credit.CryptoMerchantCredit, error) {
	return s.decide(ctx, cmd, func(ctx context.Context, doc *crypto_merchant_credit.CryptoMerchantCredit, _ ReceiptRemainder) error {
		if err := doc.Reject(cmd.Actor, cmd.Reason, s.now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, doc); err != nil {
			return err
		}
		return s.publish(ctx, doc, "rejected")
	})
}

func (s *MerchantCreditService) decide(ctx context.Context, cmd DecideMerchantCreditCommand,
	apply func(context.Context, *crypto_merchant_credit.CryptoMerchantCredit, ReceiptRemainder) error) (*crypto_merchant_credit.CryptoMerchantCredit, error) {
	return decideReceiptAttribution(ctx, s.txm, s.repo, crypto_merchant_credit.DocumentType, cmd.CreditID, cmd.ExpectedVersion, apply)
}

func (s *MerchantCreditService) post(ctx context.Context, doc *crypto_merchant_credit.CryptoMerchantCredit, actor id.ID) error {
	if err := doc.Approve(actor, s.now()); err != nil {
		return err
	}
	if err := s.posting.Post(ctx, doc, func(ctx context.Context) error { return s.repo.Update(ctx, doc) }); err != nil {
		return err
	}
	return s.publish(ctx, doc, "posted")
}

func (s *MerchantCreditService) publish(ctx context.Context, doc *crypto_merchant_credit.CryptoMerchantCredit, action string) error {
	return publishReceiptAttribution(ctx, s.outbox, crypto_merchant_credit.DocumentType, doc, action)
}
