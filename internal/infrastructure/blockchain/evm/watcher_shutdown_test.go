package evm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto"
)

func TestWatcherShutdownReleasesLeaseWithTenantContext(t *testing.T) {
	t.Parallel()

	txm := &shutdownTxManager{}
	tenantInfo := &tenant.Tenant{ID: id.New().String()}
	ctx := tenant.WithTenant(tenant.WithTxManager(t.Context(), txm), tenantInfo)
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()

	repo := &shutdownLeaseRepo{cancel: cancel, checkRelease: func(releaseCtx context.Context) {
		require.NoError(t, releaseCtx.Err(), "lease release must survive worker cancellation")
		require.Same(t, txm, tenant.MustGetTxManager(releaseCtx))
		require.Same(t, tenantInfo, tenant.GetTenant(releaseCtx))
		deadline, ok := releaseCtx.Deadline()
		require.True(t, ok, "lease release must have a bounded deadline")
		require.Positive(t, time.Until(deadline))
		require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
	}}
	watcher, err := NewWatcher(WatcherConfig{
		Network:                testSepoliaNetwork(),
		RPC:                    &fakeWatcherRPC{},
		StateRepository:        repo,
		BlockHistoryRepository: &fakeWatcherHistoryRepo{},
		BlockCommitter:         &fakeBlockCommitter{},
		NetworkHoldRecorder:    &fakeNetworkHoldRepo{},
		WatcherKey:             "native_eth",
	})
	require.NoError(t, err)

	err = watcher.Start(ctx, crypto.BlockchainEventSinkFunc(func(context.Context, crypto.BlockchainEvent) error {
		t.Fatal("cancelled watcher must not process a new event")
		return nil
	}))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, repo.releases, "acquired lease must be released once")
}

type shutdownTxManager struct{}

func (*shutdownTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type shutdownLeaseRepo struct {
	cancel       context.CancelFunc
	checkRelease func(context.Context)
	releases     int
}

var _ crypto.WatcherLeaseRepository = (*shutdownLeaseRepo)(nil)

func (*shutdownLeaseRepo) Get(_ context.Context, networkID id.ID, watcherKey string) (*crypto.WatcherState, error) {
	return &crypto.WatcherState{NetworkID: networkID, WatcherKey: watcherKey}, nil
}

func (*shutdownLeaseRepo) Save(context.Context, *crypto.WatcherState) error { return nil }

func (r *shutdownLeaseRepo) Acquire(_ context.Context, state *crypto.WatcherState, owner string) (*crypto.WatcherState, error) {
	state.LeaseOwner, state.FencingToken = owner, 1
	r.cancel()
	return state, nil
}

func (*shutdownLeaseRepo) Renew(ctx context.Context, _ *crypto.WatcherState) error { return ctx.Err() }

func (*shutdownLeaseRepo) Rewind(context.Context, *crypto.WatcherState, string) error { return nil }

func (r *shutdownLeaseRepo) Release(ctx context.Context, _ *crypto.WatcherState) error {
	r.releases++
	r.checkRelease(ctx)
	return nil
}
