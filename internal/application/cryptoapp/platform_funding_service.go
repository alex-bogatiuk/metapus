package cryptoapp

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/crypto_platform_funding"
)

type PlatformFundingRepository interface {
	LockReceiptRemainder(context.Context, id.ID) (ReceiptRemainder, error)
	// ReceiptMerchantTie is the merchant the receipt is tied to through its
	// wallet now, or nil.
	ReceiptMerchantTie(context.Context, id.ID) (*crypto_platform_funding.MerchantTie, error)
	Create(context.Context, *crypto_platform_funding.CryptoPlatformFunding) error
	GetByID(context.Context, id.ID) (*crypto_platform_funding.CryptoPlatformFunding, error)
	GetByIDForUpdate(context.Context, id.ID) (*crypto_platform_funding.CryptoPlatformFunding, error)
	Update(context.Context, *crypto_platform_funding.CryptoPlatformFunding) error
}

type PreparePlatformFundingCommand struct {
	ReceiptID              id.ID
	Reason                 string
	AcknowledgeMerchantTie bool
	Actor                  id.ID
}

type DecidePlatformFundingCommand struct {
	FundingID       id.ID
	ExpectedVersion int
	Actor           id.ID
	Reason          string
}

var errMerchantTieChanged = apperror.NewConflict("merchant tie of the receipt changed since preparation; reject and prepare the platform funding again")

// PlatformFundingService takes unidentified receipts as platform money. The
// whole remainder moves, and another operator always decides.
type PlatformFundingService struct {
	txm     tx.Manager
	repo    PlatformFundingRepository
	posting FinancialPostingEngine
	numbers numerator.Generator
	outbox  domain.OutboxPublisher
	now     func() time.Time
}

func NewPlatformFundingService(txm tx.Manager, repo PlatformFundingRepository, posting FinancialPostingEngine,
	numbers numerator.Generator, outbox domain.OutboxPublisher, now func() time.Time) *PlatformFundingService {
	if txm == nil || repo == nil || posting == nil || numbers == nil || outbox == nil || now == nil {
		panic("platform funding requires transaction, repository, posting, numbering and outbox")
	}
	return &PlatformFundingService{txm: txm, repo: repo, posting: posting, numbers: numbers, outbox: outbox, now: now}
}

func (s *PlatformFundingService) Prepare(ctx context.Context, cmd PreparePlatformFundingCommand) (*crypto_platform_funding.CryptoPlatformFunding, error) {
	var result *crypto_platform_funding.CryptoPlatformFunding
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
		tie, err := s.repo.ReceiptMerchantTie(ctx, cmd.ReceiptID)
		if err != nil {
			return err
		}
		doc, err := crypto_platform_funding.Prepare(cmd.ReceiptID, remainder.WalletID, remainder.TokenID, remainder.Amount,
			cmd.Reason, tie, cmd.AcknowledgeMerchantTie, cmd.Actor, s.now())
		if err != nil {
			return err
		}
		if doc.Number, err = s.numbers.GetNextNumber(ctx, numerator.DocumentSeries(crypto_platform_funding.Definition), doc.Date); err != nil {
			return fmt.Errorf("number platform funding: %w", err)
		}
		if err := s.repo.Create(ctx, doc); err != nil {
			return err
		}
		if err := s.publish(ctx, doc, "prepared"); err != nil {
			return err
		}
		result = doc
		return nil
	})
	return result, err
}

func (s *PlatformFundingService) Approve(ctx context.Context, cmd DecidePlatformFundingCommand) (*crypto_platform_funding.CryptoPlatformFunding, error) {
	return s.decide(ctx, cmd, func(ctx context.Context, doc *crypto_platform_funding.CryptoPlatformFunding, remainder ReceiptRemainder) error {
		if err := remainder.attributable(); err != nil {
			return err
		}
		tie, err := s.repo.ReceiptMerchantTie(ctx, doc.ReceiptID)
		if err != nil {
			return err
		}
		if !doc.TieMatches(tie) {
			return errMerchantTieChanged
		}
		if err := doc.Approve(cmd.Actor, s.now()); err != nil {
			return err
		}
		if err := s.posting.Post(ctx, doc, func(ctx context.Context) error { return s.repo.Update(ctx, doc) }); err != nil {
			return err
		}
		return s.publish(ctx, doc, "posted")
	})
}

func (s *PlatformFundingService) Reject(ctx context.Context, cmd DecidePlatformFundingCommand) (*crypto_platform_funding.CryptoPlatformFunding, error) {
	return s.decide(ctx, cmd, func(ctx context.Context, doc *crypto_platform_funding.CryptoPlatformFunding, _ ReceiptRemainder) error {
		if err := doc.Reject(cmd.Actor, cmd.Reason, s.now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, doc); err != nil {
			return err
		}
		return s.publish(ctx, doc, "rejected")
	})
}

func (s *PlatformFundingService) decide(ctx context.Context, cmd DecidePlatformFundingCommand,
	apply func(context.Context, *crypto_platform_funding.CryptoPlatformFunding, ReceiptRemainder) error) (*crypto_platform_funding.CryptoPlatformFunding, error) {
	return decideReceiptAttribution(ctx, s.txm, s.repo, crypto_platform_funding.DocumentType, cmd.FundingID, cmd.ExpectedVersion, apply)
}

func (s *PlatformFundingService) publish(ctx context.Context, doc *crypto_platform_funding.CryptoPlatformFunding, action string) error {
	return publishReceiptAttribution(ctx, s.outbox, crypto_platform_funding.DocumentType, doc, action)
}
