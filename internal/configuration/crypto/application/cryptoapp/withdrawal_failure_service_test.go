package cryptoapp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/pkg/sdk/id"
)

type withdrawalFailureFixture struct {
	state   WithdrawalFailureState
	err     error
	writes  int
	version int
}

func (f *withdrawalFailureFixture) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (f *withdrawalFailureFixture) LockWithdrawalFailure(context.Context, id.ID) (WithdrawalFailureState, error) {
	return f.state, f.err
}
func (f *withdrawalFailureFixture) ReleaseWithdrawalReservation(context.Context, id.ID) error {
	f.writes++
	return nil
}
func (f *withdrawalFailureFixture) MarkWithdrawalFailed(context.Context, id.ID, string) error {
	f.writes++
	f.state.Status = "failed"
	return nil
}
func (f *withdrawalFailureFixture) MarkWithdrawalExecutionFailed(context.Context, id.ID, string) error {
	f.writes++
	return nil
}
func (f *withdrawalFailureFixture) StornoMovements(_ context.Context, _ id.ID, version int) error {
	f.writes++
	f.version = version
	return nil
}

func TestWithdrawalFailureRequiresProofBeforeCompensation(t *testing.T) {
	for _, test := range []struct {
		name    string
		state   WithdrawalFailureState
		readErr error
		wantErr bool
	}{
		{"authorization issued", WithdrawalFailureState{Status: "signing", HasSigningAttempt: true}, nil, true},
		{"execution hash", WithdrawalFailureState{Status: "signing", HasExecutionEffect: true}, nil, true},
		{"read unavailable", WithdrawalFailureState{}, errors.New("database unavailable"), true},
		{"already compensated", WithdrawalFailureState{Status: "failed"}, nil, false},
		{"broadcast", WithdrawalFailureState{Status: "broadcast"}, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &withdrawalFailureFixture{state: test.state, err: test.readErr}
			changed, err := NewWithdrawalFailureService(f, f, f).FailWithdrawalBeforeEffect(context.Background(), id.New(), "expiry")
			require.Equal(t, test.wantErr, err != nil)
			require.False(t, changed)
			require.Zero(t, f.writes)
		})
	}
}

func TestWithdrawalFailureUsesLockedVersionAndIsIdempotent(t *testing.T) {
	f := &withdrawalFailureFixture{state: WithdrawalFailureState{Status: "signing", PostedVersion: 7}}
	s := NewWithdrawalFailureService(f, f, f)
	changed, err := s.FailWithdrawalBeforeEffect(context.Background(), id.New(), "expiry")
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, 8, f.version)
	changed, err = s.FailWithdrawalBeforeEffect(context.Background(), id.New(), "expiry")
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, 4, f.writes)
}
