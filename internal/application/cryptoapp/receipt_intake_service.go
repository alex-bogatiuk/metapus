package cryptoapp

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_custody_receipt"
	"metapus/internal/domain/documents/crypto_payment"
)

type ReceiptAdmissionContext struct {
	EffectID          id.ID
	CustodyMerchantID *id.ID
}

type ReceiptIntakeRepository interface {
	LockAdmission(context.Context, crypto.AccountingTransferBasis) error
	// FindByEffect returns the receipt of the logical effect of an occurrence.
	FindByEffect(context.Context, id.ID) (*crypto_custody_receipt.CryptoCustodyReceipt, error)
	AdmissionContext(context.Context, crypto.AccountingTransferBasis, crypto.VerifiedCustodyEffect) (ReceiptAdmissionContext, error)
	Create(context.Context, *crypto_custody_receipt.CryptoCustodyReceipt) error
	Update(context.Context, *crypto_custody_receipt.CryptoCustodyReceipt) error
	PrepareRecognition(context.Context, *crypto_custody_receipt.CryptoCustodyReceipt, ReceiptAdmissionContext) error
	AppendIntakeAudit(context.Context, *crypto_custody_receipt.CryptoCustodyReceipt) error
	// AnchorAudit moves the canonicality audit of a re-proven receipt to the
	// proof of the block its transfer is now in.
	AnchorAudit(ctx context.Context, receiptID, verificationID id.ID) error
}

type RecordCustodyReceiptCommand struct {
	Basis  crypto.AccountingTransferBasis
	Event  crypto.BlockchainEvent
	Reason crypto_custody_receipt.Reason
}

// ReceiptIntakeService records a final, independently proven external receipt
// as custody plus suspense. The economic purpose (merchant credit, treasury,
// refund) is decided later by separate documents consuming the suspense.
type ReceiptIntakeService struct {
	txm     tx.Manager
	repo    ReceiptIntakeRepository
	guard   crypto.CustodyEffectGuard
	posting FinancialPostingEngine
	numbers numerator.Generator
	outbox  domain.OutboxPublisher
	now     func() time.Time
}

func NewReceiptIntakeService(
	txm tx.Manager, repo ReceiptIntakeRepository, guard crypto.CustodyEffectGuard,
	posting FinancialPostingEngine, numbers numerator.Generator, outbox domain.OutboxPublisher, now func() time.Time,
) *ReceiptIntakeService {
	if txm == nil || repo == nil || guard == nil || posting == nil || numbers == nil || outbox == nil || now == nil {
		panic("receipt intake requires transactional evidence, posting, numbering and outbox")
	}
	return &ReceiptIntakeService{txm: txm, repo: repo, guard: guard, posting: posting, numbers: numbers, outbox: outbox, now: now}
}

type custodyReceiptPostedPayload struct {
	EntityType string                                       `json:"entityType"`
	EntityID   string                                       `json:"entityId"`
	Action     string                                       `json:"action"`
	Doc        *crypto_custody_receipt.CryptoCustodyReceipt `json:"doc"`
}

func (s *ReceiptIntakeService) Record(ctx context.Context, req RecordCustodyReceiptCommand) (*crypto_custody_receipt.CryptoCustodyReceipt, error) {
	if err := req.Basis.Validate(); err != nil {
		return nil, err
	}
	if !req.Reason.Valid() {
		return nil, apperror.NewValidation("receipt reason is required")
	}
	var result *crypto_custody_receipt.CryptoCustodyReceipt
	err := s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.LockAdmission(ctx, req.Basis); err != nil {
			return err
		}
		evidence, err := s.guard.AuthorizeCustodyEffect(ctx, req.Basis, req.Event)
		if err != nil {
			return err
		}
		existing, err := s.repo.FindByEffect(ctx, evidence.OccurrenceID)
		if err != nil {
			return err
		}
		if existing != nil {
			if !existing.Posted || existing.NetworkID != req.Basis.NetworkID ||
				existing.WalletID != req.Basis.WalletID || existing.TokenID != req.Basis.TokenID ||
				existing.Amount.Cmp(req.Basis.Amount) != 0 || existing.ReasonCode != req.Reason {
				return apperror.NewConflict("receipt replay differs from recorded evidence")
			}
			// A re-included transfer keeps its receipt; the audit follows it
			// into the new block so it does not report the old block absent.
			if evidence.VerificationID != existing.VerificationID {
				if err := s.repo.AnchorAudit(ctx, existing.ID, evidence.VerificationID); err != nil {
					return err
				}
			}
			result = existing
			return nil
		}
		admission, err := s.repo.AdmissionContext(ctx, req.Basis, evidence)
		if err != nil {
			return err
		}
		doc := crypto_custody_receipt.New()
		doc.Date = s.now().UTC()
		doc.NetworkID, doc.WalletID, doc.TokenID = req.Basis.NetworkID, req.Basis.WalletID, req.Basis.TokenID
		doc.CustodyMerchantID = admission.CustodyMerchantID
		doc.VerificationID, doc.OccurrenceID = evidence.VerificationID, evidence.OccurrenceID
		doc.PostingID = id.New()
		doc.Amount, doc.ReasonCode = req.Basis.Amount, req.Reason
		doc.BasisType, doc.BasisID = "CryptoTransferVerification", &doc.VerificationID
		doc.Number, err = s.numbers.GetNextNumber(ctx, numerator.DefaultConfig("CCR"), doc.Date)
		if err != nil {
			return fmt.Errorf("number custody receipt: %w", err)
		}
		if err := doc.Validate(ctx); err != nil {
			return err
		}
		if err := s.repo.PrepareRecognition(ctx, doc, admission); err != nil {
			return err
		}
		if err := s.repo.Create(ctx, doc); err != nil {
			return err
		}
		if err := s.posting.Post(ctx, doc, func(ctx context.Context) error { return s.repo.Update(ctx, doc) }); err != nil {
			return err
		}
		if err := s.repo.AppendIntakeAudit(ctx, doc); err != nil {
			return err
		}
		if err := s.outbox.Publish(ctx, domain.DomainEvent{
			AggregateType: crypto_custody_receipt.DocumentType, AggregateID: doc.ID, EventType: "posted",
			Payload: custodyReceiptPostedPayload{EntityType: crypto_custody_receipt.DocumentType, EntityID: doc.ID.String(), Action: "posted", Doc: doc},
		}); err != nil {
			return err
		}
		result = doc
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RecordPaymentReceipt accounts the proven final transfer of a matched invoice
// payment. The payment then attributes the receipt instead of owning custody.
func (s *ReceiptIntakeService) RecordPaymentReceipt(ctx context.Context, payment *crypto_payment.CryptoPayment, event crypto.BlockchainEvent) (id.ID, error) {
	if payment == nil || payment.VerificationID == nil {
		return id.ID{}, crypto.ErrVerificationPending
	}
	doc, err := s.Record(ctx, RecordCustodyReceiptCommand{
		Basis: crypto.AccountingTransferBasis{
			NetworkID: payment.NetworkID, WalletID: payment.WalletID, TokenID: payment.TokenID,
			TxHash: payment.TxHash, EventKind: crypto.EventKind(payment.EventKind), EventIndex: payment.EventIndex,
			FromAddress: payment.FromAddress, RecordType: entity.RecordTypeReceipt, Amount: payment.Amount,
			BlockNumber: payment.BlockNumber, BlockHash: payment.BlockHash, RequiredConfs: payment.RequiredConfs,
			VerificationID: payment.VerificationID,
		},
		Event:  event,
		Reason: crypto_custody_receipt.ReasonMatched,
	})
	if err != nil {
		return id.ID{}, err
	}
	return doc.ID, nil
}

var _ crypto.PaymentReceiptRecorder = (*ReceiptIntakeService)(nil)
