package outboundapp

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
)

// FundingEvidence runs before settlement, outside its financial transaction.
// A final transaction receipt alone does not prove the native principal effect.
type FundingEvidence interface {
	VerifyFunding(context.Context, crypto.NetworkDriver, crypto.NativeFunding, crypto.ChainReceipt) error
}

type FundingEvidenceRepository interface {
	ExpectedFundingTransfer(context.Context, id.ID, crypto.ChainReceipt) (crypto.BlockchainEvent, error)
	SaveVerification(context.Context, crypto.TransferVerification) (id.ID, error)
}

type NativeFundingEvidenceService struct{ repository FundingEvidenceRepository }

func NewNativeFundingEvidenceService(repository FundingEvidenceRepository) *NativeFundingEvidenceService {
	return &NativeFundingEvidenceService{repository: repository}
}

func (s *NativeFundingEvidenceService) VerifyFunding(ctx context.Context, driver crypto.NetworkDriver, funding crypto.NativeFunding, receipt crypto.ChainReceipt) error {
	if receipt.Status != crypto.ReceiptSucceeded {
		return nil // A reverted transaction has fees, but no principal.
	}
	verifier, ok := driver.(crypto.TransferVerifier)
	if s == nil || s.repository == nil || !ok {
		return crypto.ErrVerificationPending
	}
	event, err := s.repository.ExpectedFundingTransfer(ctx, funding.ID, receipt)
	if err != nil {
		return err
	}
	proof, err := verifier.VerifyTransfer(ctx, event)
	if err != nil {
		return err
	}
	if err := proof.Validate(); err != nil {
		return err
	}
	if proof.AccountingIdentity == nil || proof.Event.Canonicality != crypto.CanonicalityCanonical ||
		proof.Event.BlockHash != receipt.BlockHash || proof.Event.BlockNumber != receipt.BlockHeight ||
		proof.Event.NetworkID != event.NetworkID {
		return crypto.ErrVerificationMismatch
	}
	expectedOccurrence, err := proof.AccountingIdentity.Occurrence(event)
	if err != nil {
		return err
	}
	actualOccurrence, err := proof.AccountingIdentity.Occurrence(proof.Event)
	if err != nil || expectedOccurrence != actualOccurrence {
		return crypto.ErrVerificationMismatch
	}
	_, err = s.repository.SaveVerification(ctx, proof)
	return err
}
