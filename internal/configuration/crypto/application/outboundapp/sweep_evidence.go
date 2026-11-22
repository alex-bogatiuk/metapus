package outboundapp

import (
	"context"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
)

type SweepEvidenceRepository interface {
	ExpectedSweepTransfer(context.Context, id.ID, crypto.ChainReceipt) (crypto.BlockchainEvent, error)
	SaveVerification(context.Context, crypto.TransferVerification) (id.ID, error)
}

type SweepEvidenceService struct{ repository SweepEvidenceRepository }

func NewSweepEvidenceService(repository SweepEvidenceRepository) *SweepEvidenceService {
	return &SweepEvidenceService{repository: repository}
}

func (s *SweepEvidenceService) VerifySweep(ctx context.Context, driver crypto.NetworkDriver, task SettlementTask, receipt crypto.ChainReceipt) (id.ID, error) {
	verifier, ok := driver.(crypto.ExpectedTransferVerifier)
	if !ok || s == nil || s.repository == nil {
		return id.ID{}, crypto.ErrVerificationPending
	}
	event, err := s.repository.ExpectedSweepTransfer(ctx, task.LineID, receipt)
	if err != nil {
		return id.ID{}, err
	}
	proof, err := verifier.VerifyExpectedTransfer(ctx, event)
	if err != nil {
		return id.ID{}, err
	}
	if err := validateExpectedTransferProof(event, proof); err != nil {
		return id.ID{}, err
	}
	return s.repository.SaveVerification(ctx, proof)
}

func validateExpectedTransferProof(event crypto.BlockchainEvent, proof crypto.TransferVerification) error {
	if err := proof.Validate(); err != nil {
		return err
	}
	if proof.AccountingIdentity == nil || proof.Event.Canonicality != crypto.CanonicalityCanonical || proof.ExecutionStatus != crypto.ReceiptSucceeded {
		return crypto.ErrVerificationMismatch
	}
	if event.EventKind == crypto.EventKindNativeValue && proof.MatchingEffects != 1 {
		return crypto.ErrVerificationMismatch
	}
	event.EventIndex = proof.Event.EventIndex
	expected, err := proof.AccountingIdentity.Occurrence(event)
	if err != nil {
		return err
	}
	actual, err := proof.AccountingIdentity.Occurrence(proof.Event)
	if err != nil || actual != expected {
		return crypto.ErrVerificationMismatch
	}
	return nil
}
