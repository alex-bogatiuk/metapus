package cryptoapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tx"
)

var ErrWithdrawalMayHaveExternalEffect = errors.New("withdrawal requires recovery: an authorization may have an external effect")

type WithdrawalFailureState struct {
	Status             string
	PostedVersion      int
	HasSigningAttempt  bool
	HasExecutionEffect bool
}

type WithdrawalFailureRepository interface {
	LockWithdrawalFailure(context.Context, id.ID) (WithdrawalFailureState, error)
	ReleaseWithdrawalReservation(context.Context, id.ID) error
	MarkWithdrawalFailed(context.Context, id.ID, string) error
	MarkWithdrawalExecutionFailed(context.Context, id.ID, string) error
}

type WithdrawalBalanceReversal interface {
	StornoMovements(context.Context, id.ID, int) error
}

// WithdrawalFailureService only compensates a provably pre-effect request.
// The repository lock is also taken when publishing a signing authorization.
type WithdrawalFailureService struct {
	txm      tx.Manager
	repo     WithdrawalFailureRepository
	balances WithdrawalBalanceReversal
}

func NewWithdrawalFailureService(txm tx.Manager, repo WithdrawalFailureRepository, balances WithdrawalBalanceReversal) *WithdrawalFailureService {
	if txm == nil || repo == nil || balances == nil {
		panic("withdrawal failure requires transactional ports")
	}
	return &WithdrawalFailureService{txm: txm, repo: repo, balances: balances}
}

func (s *WithdrawalFailureService) FailWithdrawalBeforeEffect(ctx context.Context, requestID id.ID, reason string) (changed bool, err error) {
	err = s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		state, err := s.repo.LockWithdrawalFailure(ctx, requestID)
		if err != nil {
			return err
		}
		if state.Status == "failed" {
			return nil
		}
		if state.HasSigningAttempt || state.HasExecutionEffect {
			return ErrWithdrawalMayHaveExternalEffect
		}
		if state.Status != "approved" && state.Status != "signing" {
			return fmt.Errorf("withdrawal cannot fail before effect from %s", state.Status)
		}
		if err := s.repo.ReleaseWithdrawalReservation(ctx, requestID); err != nil {
			return err
		}
		if err := s.balances.StornoMovements(ctx, requestID, state.PostedVersion+1); err != nil {
			return err
		}
		if err := s.repo.MarkWithdrawalFailed(ctx, requestID, reason); err != nil {
			return err
		}
		if err := s.repo.MarkWithdrawalExecutionFailed(ctx, requestID, reason); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		changed = false
	}
	return changed, err
}
