package cryptoapp

import (
	"context"
	"strings"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/posting"
)

type PaymentRecognitionResult struct {
	PostingID     id.ID `json:"postingId"`
	PaymentID     id.ID `json:"paymentId"`
	Generation    int   `json:"generation"`
	PostedVersion int   `json:"postedVersion"`
	Existing      bool  `json:"existing"`
}

type PaymentRecognitionInput struct {
	Payment                *crypto_payment.CryptoPayment
	Evidence               crypto.VerifiedCustodyEffect
	Movement               entity.CryptoBalanceMovement
	Mode, Actor, Reference string
}

type PaymentRecognitionRepository interface {
	RequiresRecognition(context.Context, *crypto_payment.CryptoPayment) (bool, error)
	Authorize(context.Context, *crypto_payment.CryptoPayment, id.ID) (crypto.VerifiedCustodyEffect, error)
	Claim(context.Context, PaymentRecognitionInput) (PaymentRecognitionResult, error)
	LoadForAdoption(context.Context, id.ID) (*crypto_payment.CryptoPayment, error)
	OriginalMovement(context.Context, *crypto_payment.CryptoPayment) (entity.CryptoBalanceMovement, error)
}

// PaymentRecognitionService binds v1's existing physical/merchant/fee set to a
// shared custody owner. It neither allocates suspense nor changes v1 fee policy.
type PaymentRecognitionService struct {
	txm    tx.Manager
	repo   PaymentRecognitionRepository
	outbox domain.OutboxPublisher
}

func NewPaymentRecognitionService(txm tx.Manager, repo PaymentRecognitionRepository, outbox domain.OutboxPublisher) *PaymentRecognitionService {
	if txm == nil || repo == nil || outbox == nil {
		panic("payment recognition requires transaction, repository and outbox")
	}
	return &PaymentRecognitionService{txm: txm, repo: repo, outbox: outbox}
}

// ValidatePosting takes scope/balance/effect locks before reversals and money.
func (s *PaymentRecognitionService) ValidatePosting(ctx context.Context, doc posting.Postable) error {
	p, ok := doc.(*crypto_payment.CryptoPayment)
	if !ok {
		return nil
	}
	required, err := s.repo.RequiresRecognition(ctx, p)
	if err != nil || !required {
		return err
	}
	if p.VerificationID == nil {
		return crypto.ErrVerificationPending
	}
	_, err = s.repo.Authorize(ctx, p, *p.VerificationID)
	return err
}

func (s *PaymentRecognitionService) PrepareMovements(ctx context.Context, doc posting.Postable, set *posting.MovementSet) error {
	p, ok := doc.(*crypto_payment.CryptoPayment)
	if !ok {
		return nil
	}
	required, err := s.repo.RequiresRecognition(ctx, p)
	if err != nil || !required {
		return err
	}
	if p.VerificationID == nil {
		return crypto.ErrVerificationPending
	}
	rows, err := posting.CryptoBalanceMovements(set)
	if err != nil {
		return err
	}
	if len(rows) != 1 || rows[0].RecorderID != p.ID || rows[0].RecorderType != crypto_payment.Definition.Name ||
		rows[0].RecorderVersion != p.PostedVersion+1 || rows[0].RecordType != entity.RecordTypeReceipt ||
		rows[0].WalletID != p.WalletID || rows[0].TokenID != p.TokenID || !rows[0].Amount.Equal(p.Amount) {
		return apperror.NewValidation("payment recognition requires its exact physical movement")
	}
	// Domain movement plans leave recording metadata to the adapter. Reserve
	// the identity in the shared slice before the repository stamps/copies it.
	if id.IsNil(rows[0].LineID) {
		rows[0].LineID = id.New()
	}
	evidence, err := s.repo.Authorize(ctx, p, *p.VerificationID)
	if err != nil {
		return err
	}
	_, err = s.claim(ctx, PaymentRecognitionInput{Payment: p, Evidence: evidence, Movement: rows[0], Mode: "posting", Actor: "payment-posting", Reference: "verified-payment-posting"})
	return err
}

type AdoptPaymentRecognitionCommand struct {
	PaymentID, VerificationID id.ID
	Actor, Reference          string
}

// Adopt links a currently posted legacy generation without generating money or
// rewriting the payment/proof. The repository enforces a disabled drained network.
func (s *PaymentRecognitionService) Adopt(ctx context.Context, cmd AdoptPaymentRecognitionCommand) (PaymentRecognitionResult, error) {
	var result PaymentRecognitionResult
	if id.IsNil(cmd.PaymentID) || id.IsNil(cmd.VerificationID) || strings.TrimSpace(cmd.Actor) == "" || strings.TrimSpace(cmd.Reference) == "" {
		return result, apperror.NewValidation("payment adoption requires identity, verification and attestation")
	}
	err := s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		p, err := s.repo.LoadForAdoption(ctx, cmd.PaymentID)
		if err != nil {
			return err
		}
		if !p.Posted || p.PostedVersion < 1 || p.Canonicality != "canonical" ||
			(p.Status != crypto_payment.PaymentStatusConfirmed && p.Status != crypto_payment.PaymentStatusSettled) {
			return apperror.NewConflict("adoption requires a currently posted canonical payment")
		}
		evidence, err := s.repo.Authorize(ctx, p, cmd.VerificationID)
		if err != nil {
			return err
		}
		movement, err := s.repo.OriginalMovement(ctx, p)
		if err != nil {
			return err
		}
		result, err = s.claim(ctx, PaymentRecognitionInput{Payment: p, Evidence: evidence, Movement: movement, Mode: "adoption", Actor: cmd.Actor, Reference: cmd.Reference})
		return err
	})
	if err != nil {
		return PaymentRecognitionResult{}, err
	}
	return result, nil
}

type paymentRecognizedPayload struct {
	EntityType    string `json:"entityType"`
	EntityID      string `json:"entityId"`
	Action        string `json:"action"`
	PostingID     id.ID  `json:"postingId"`
	Generation    int    `json:"generation"`
	PostedVersion int    `json:"postedVersion"`
	Mode          string `json:"mode"`
}

func (s *PaymentRecognitionService) claim(ctx context.Context, input PaymentRecognitionInput) (PaymentRecognitionResult, error) {
	result, err := s.repo.Claim(ctx, input)
	if err != nil || result.Existing {
		return result, err
	}
	err = s.outbox.Publish(ctx, domain.DomainEvent{AggregateType: "CryptoPayment", AggregateID: result.PaymentID, EventType: "custody_recognized",
		Payload: paymentRecognizedPayload{EntityType: "CryptoPayment", EntityID: result.PaymentID.String(), Action: "custody_recognized",
			PostingID: result.PostingID, Generation: result.Generation, PostedVersion: result.PostedVersion, Mode: input.Mode}})
	return result, err
}
