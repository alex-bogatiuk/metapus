package cryptoapp

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/aml"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tx"
)

type AMLBatchSweepPlanningRepository interface {
	ClaimAMLBatchSweepCandidate(context.Context) (aml.BatchSweepRequest, bool, error)
	ReserveAMLBatchSweep(context.Context, aml.BatchSweepRequest) (aml.InternalAllocation, error)
	MarkAMLSourceSweeping(context.Context, id.ID) (bool, error)
}
type AMLBatchSweepPlanner struct {
	repo AMLBatchSweepPlanningRepository
	txm  tx.Manager
}

func NewAMLBatchSweepPlanner(repo AMLBatchSweepPlanningRepository, txm tx.Manager) *AMLBatchSweepPlanner {
	return &AMLBatchSweepPlanner{repo, txm}
}

// RunOnce reserves the candidate's exact revisions and advances the source in
// the same transaction. The SQL adapter owns eligibility projection and locks.
func (s *AMLBatchSweepPlanner) RunOnce(ctx context.Context) (bool, error) {
	planned := false
	err := s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		request, found, err := s.repo.ClaimAMLBatchSweepCandidate(ctx)
		if err != nil || !found {
			return err
		}
		request.SweepAttemptID = id.New()
		if _, err := s.repo.ReserveAMLBatchSweep(ctx, request); err != nil {
			return err
		}
		changed, err := s.repo.MarkAMLSourceSweeping(ctx, request.SourceWalletID)
		if err != nil {
			return err
		}
		if !changed {
			return apperror.NewConflict("AML source wallet status changed")
		}
		planned = true
		return nil
	})
	return planned, err
}
