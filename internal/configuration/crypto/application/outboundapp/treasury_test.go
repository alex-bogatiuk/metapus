package outboundapp

import (
	"context"
	"errors"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/stretchr/testify/require"
	"testing"
)

type treasuryStoreStub struct {
	TreasuryRepository
	err     error
	lookups int
}

func (s *treasuryStoreStub) GetTreasurySigningAttempt(context.Context, id.ID) (HotWallet, SigningAuthorizationRecord, bool, error) {
	s.lookups++
	return HotWallet{}, SigningAuthorizationRecord{}, false, s.err
}

func TestTreasuryRecoveryOnlyCannotCreateNewFinancialEffect(t *testing.T) {
	store := &treasuryStoreStub{}
	executor := NewTreasuryExecutor(nil, nil, nil, store, nil, nil, nil, nil, nil, nil, nil, "tenant", nil, nil, nil)
	err := executor.Execute(t.Context(), TreasuryTask{WithdrawalID: id.New(), RecoveryOnly: true})
	require.True(t, IsHeldExecutionError(err))
	require.ErrorContains(t, err, "matching financial finality")
	require.Equal(t, 1, store.lookups)
	// Any reservation, signer or broadcaster call would panic: none is configured.
	store.err = errors.New("storage unavailable")
	err = executor.Execute(t.Context(), TreasuryTask{WithdrawalID: id.New()})
	require.ErrorIs(t, err, store.err)
}
