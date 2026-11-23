package cryptoapp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

type adminRejectionFixture struct {
	status        withdrawal_request.Status
	postedVersion int
	stornoVersion int
	stornoErr     error
	steps         []string
}

func (f *adminRejectionFixture) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	f.steps = append(f.steps, "transaction")
	return fn(ctx)
}

func (f *adminRejectionFixture) LockWithdrawalRequestForAdminRejection(_ context.Context, requestID id.ID) (id.ID, int, withdrawal_request.Status, error) {
	f.steps = append(f.steps, "lock")
	return requestID, f.postedVersion, f.status, nil
}

func (f *adminRejectionFixture) StornoMovements(_ context.Context, _ id.ID, version int) error {
	f.steps = append(f.steps, "storno")
	f.stornoVersion = version
	return f.stornoErr
}

func (f *adminRejectionFixture) RejectAdminWithdrawalRequest(_ context.Context, _, _ id.ID, _ string) error {
	f.steps = append(f.steps, "reject")
	return nil
}

func TestAdminWithdrawalRejectionCompensatesBeforeStateChange(t *testing.T) {
	f := &adminRejectionFixture{status: withdrawal_request.StatusPendingApproval, postedVersion: 4}
	ctx := tenant.WithTxManager(context.Background(), f)
	err := NewWithdrawalRejectionService(f, f).Reject(ctx, id.New(), id.New(), "Denied")
	require.NoError(t, err)
	require.Equal(t, 5, f.stornoVersion)
	require.Equal(t, []string{"transaction", "lock", "storno", "reject"}, f.steps)
}

func TestAdminWithdrawalRejectionDoesNotChangeStateWithoutCompensation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status withdrawal_request.Status
		err    error
		steps  []string
	}{
		{"already approved", withdrawal_request.StatusApproved, nil, []string{"transaction", "lock"}},
		{"storno failed", withdrawal_request.StatusPendingApproval, errors.New("balance unavailable"), []string{"transaction", "lock", "storno"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &adminRejectionFixture{status: tc.status, stornoErr: tc.err}
			ctx := tenant.WithTxManager(context.Background(), f)
			err := NewWithdrawalRejectionService(f, f).Reject(ctx, id.New(), id.New(), "Denied")
			require.Error(t, err)
			require.Equal(t, tc.steps, f.steps)
		})
	}
}

func TestAdminWithdrawalRejectionRequiresReasonBeforeTransaction(t *testing.T) {
	f := &adminRejectionFixture{status: withdrawal_request.StatusPendingApproval}
	err := NewWithdrawalRejectionService(f, f).Reject(context.Background(), id.New(), id.New(), "  ")
	require.Error(t, err)
	require.Empty(t, f.steps)
}
