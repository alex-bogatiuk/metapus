package cryptoapp

import (
	"context"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tx"
)

type SweepLifecycleRepository interface {
	LockSweepWallets(context.Context, []id.ID) error
	MarkSweepAttemptsPosted(context.Context, []id.ID) error
	MarkWalletsFreeAfterSweep(context.Context, []id.ID) error
	PrepareSweepMaintenance(context.Context) (SweepMaintenanceBatch, error)
	ExpireUnsignedSweepAttempts(context.Context, SweepMaintenanceBatch) error
	CloseAbandonedSweepDrafts(context.Context, SweepMaintenanceBatch) error
	RecoverIdleSweepWallets(context.Context, SweepMaintenanceBatch) error
}

// A maintenance transaction freezes its candidate identities before locking
// their combined resources. Eligibility is checked again by every mutation;
// newly eligible rows wait for the next batch instead of adding late locks.
type SweepMaintenanceBatch struct {
	Authorizations, Fees, Budgets, Attempts, Allocations, Drafts, Wallets []id.ID
}

// SweepLifecycleService owns the transaction joining attempt completion and
// wallet reuse. A wallet is a shared resource, not the property of one token.
type SweepLifecycleService struct {
	txm  tx.Manager
	repo SweepLifecycleRepository
}

func NewSweepLifecycleService(txm tx.Manager, repo SweepLifecycleRepository) *SweepLifecycleService {
	if txm == nil || repo == nil {
		panic("sweep lifecycle requires transactional ports")
	}
	return &SweepLifecycleService{txm: txm, repo: repo}
}

func (s *SweepLifecycleService) Complete(ctx context.Context, wallets, attempts []id.ID) error {
	return s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.LockSweepWallets(ctx, wallets); err != nil {
			return err
		}
		if err := s.repo.MarkSweepAttemptsPosted(ctx, attempts); err != nil {
			return err
		}
		return s.repo.MarkWalletsFreeAfterSweep(ctx, wallets)
	})
}

func (s *SweepLifecycleService) Maintain(ctx context.Context) error {
	return s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		batch, err := s.repo.PrepareSweepMaintenance(ctx)
		if err != nil {
			return err
		}
		if err := s.repo.ExpireUnsignedSweepAttempts(ctx, batch); err != nil {
			return err
		}
		if err := s.repo.CloseAbandonedSweepDrafts(ctx, batch); err != nil {
			return err
		}
		return s.repo.RecoverIdleSweepWallets(ctx, batch)
	})
}
