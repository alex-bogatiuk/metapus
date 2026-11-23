package cryptoapp

import (
	"context"
	"errors"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/aml"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/stretchr/testify/require"
	"testing"
)

type planningTransaction struct{ committed bool }

func (s *planningTransaction) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	err := fn(ctx)
	s.committed = err == nil
	return err
}

type planningStore struct {
	found, changed bool
	reserveErr     error
	calls          []string
}

func (s *planningStore) ClaimAMLBatchSweepCandidate(context.Context) (aml.BatchSweepRequest, bool, error) {
	s.calls = append(s.calls, "claim")
	return aml.BatchSweepRequest{SourceWalletID: id.New()}, s.found, nil
}
func (s *planningStore) ReserveAMLBatchSweep(_ context.Context, r aml.BatchSweepRequest) (aml.InternalAllocation, error) {
	if id.IsNil(r.SweepAttemptID) {
		panic("missing immutable attempt identity")
	}
	s.calls = append(s.calls, "reserve")
	return aml.InternalAllocation{}, s.reserveErr
}
func (s *planningStore) MarkAMLSourceSweeping(context.Context, id.ID) (bool, error) {
	s.calls = append(s.calls, "advance")
	return s.changed, nil
}

func TestBatchSweepPlanCommitsReservationAndSourceTogether(t *testing.T) {
	for _, tt := range []struct {
		name           string
		found, changed bool
		reserveErr     error
		wantError      bool
		calls          []string
	}{
		{"empty", false, false, nil, false, []string{"claim"}},
		{"reserved", true, true, nil, false, []string{"claim", "reserve", "advance"}},
		{"stale source", true, false, nil, true, []string{"claim", "reserve", "advance"}},
		{"reservation rejected", true, false, errors.New("stale risk revision"), true, []string{"claim", "reserve"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &planningStore{found: tt.found, changed: tt.changed, reserveErr: tt.reserveErr}
			manager := &planningTransaction{}
			planned, err := NewAMLBatchSweepPlanner(store, manager).RunOnce(t.Context())
			require.Equal(t, tt.wantError, err != nil)
			require.Equal(t, !tt.wantError, manager.committed)
			require.Equal(t, tt.found && !tt.wantError, planned)
			require.Equal(t, tt.calls, store.calls)
		})
	}
}
