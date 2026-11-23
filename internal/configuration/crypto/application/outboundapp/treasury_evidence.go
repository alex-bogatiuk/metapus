package outboundapp

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type TreasuryEvidenceRepository interface {
	ExpectedTreasuryTransfer(context.Context, id.ID, crypto.ChainReceipt) (crypto.BlockchainEvent, error)
	SaveVerification(context.Context, crypto.TransferVerification) (id.ID, error)
}

type TreasuryEvidenceService struct{ repository TreasuryEvidenceRepository }

func NewTreasuryEvidenceService(repository TreasuryEvidenceRepository) *TreasuryEvidenceService {
	return &TreasuryEvidenceService{repository: repository}
}

func (s *TreasuryEvidenceService) VerifyTreasury(ctx context.Context, driver crypto.NetworkDriver, task SettlementTask, receipt crypto.ChainReceipt) (id.ID, error) {
	verifier, ok := driver.(crypto.ExpectedTransferVerifier)
	if !ok || s == nil || s.repository == nil {
		return id.ID{}, crypto.ErrVerificationPending
	}
	event, err := s.repository.ExpectedTreasuryTransfer(ctx, task.DocumentID, receipt)
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
