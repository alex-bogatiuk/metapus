package outboundapp

import (
	"context"
	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"testing"
)

type completionTransaction struct{}

func (completionTransaction) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type completionStore struct {
	WithdrawalRepository
	state WithdrawalCompletionState
}

func (s completionStore) LockWithdrawalCompletion(context.Context, WithdrawalTask) (WithdrawalCompletionState, error) {
	return s.state, nil
}

func TestWithdrawalCompletionNeverRepeatsFinalOrInvalidEffects(t *testing.T) {
	for _, tt := range []struct {
		status    string
		aml, held bool
	}{
		{"broadcast", false, false}, {"confirmed", false, false}, {"failed", false, false},
		{"broadcast", true, false}, {"confirmed", true, false}, {"failed", true, false}, {"cancelled", true, true},
	} {
		store := completionStore{state: WithdrawalCompletionState{Status: tt.status}}
		executor := NewWithdrawalExecutor(WithdrawalConfig{Repository: store, Transactions: completionTransaction{}})
		task := WithdrawalTask{RequestID: id.New()}
		if tt.aml {
			task.AMLParentID = id.New()
		}
		err := executor.completeWithdrawalBroadcast(t.Context(), task, crypto.SigningAuthorization{}, id.New(), "saved-hash")
		require.Equal(t, tt.held, IsHeldExecutionError(err), tt.status)
		// Documents, fee reservations and posting are deliberately absent: a
		// repeated/final/invalid effect must not reach any of these dependencies.
	}
}
