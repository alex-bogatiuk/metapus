package outboundapp

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type WithdrawalEvidenceRepository interface {
	ExpectedWithdrawalTransfer(context.Context, id.ID, crypto.ChainReceipt) (crypto.BlockchainEvent, error)
	SaveVerification(context.Context, crypto.TransferVerification) (id.ID, error)
}

type WithdrawalEvidenceService struct{ repository WithdrawalEvidenceRepository }

func NewWithdrawalEvidenceService(repository WithdrawalEvidenceRepository) *WithdrawalEvidenceService {
	return &WithdrawalEvidenceService{repository: repository}
}

func (s *WithdrawalEvidenceService) VerifyWithdrawal(ctx context.Context, driver crypto.NetworkDriver, task SettlementTask, receipt crypto.ChainReceipt) (id.ID, error) {
	verifier, ok := driver.(crypto.ExpectedTransferVerifier)
	if !ok || s == nil || s.repository == nil {
		return id.ID{}, crypto.ErrVerificationPending
	}
	event, err := s.repository.ExpectedWithdrawalTransfer(ctx, task.DocumentID, receipt)
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
