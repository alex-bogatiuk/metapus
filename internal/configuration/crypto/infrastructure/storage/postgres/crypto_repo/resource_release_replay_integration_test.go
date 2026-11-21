//go:build integration

package crypto_repo_test

import (
	"context"
	"sync"
	"testing"

	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/outbound_repo"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/stretchr/testify/require"
)

func TestResourceReleaseConcurrentReplayPreservesFirstDecision(t *testing.T) {
	f := newRentalReplayFixture(t)
	_, a := reserveRentalAllocation(t, f)
	repo := outbound_repo.NewOutboundRepo()
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() { errs <- repo.ReleaseResourceAllocation(f.Ctx, a.ID, "original release") })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	before := allocationSnapshot(t, f, a.ID)
	require.NoError(t, repo.ReleaseResourceAllocation(f.Ctx, a.ID, "original release"))
	require.Equal(t, before, allocationSnapshot(t, f, a.ID))
	require.ErrorContains(t, repo.ReleaseResourceAllocation(f.Ctx, a.ID, "another release"), "original reason")
	var count int
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT count(*) FROM reg_crypto_resource_allocation_events WHERE allocation_id=$1 AND event_type='released'`, a.ID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestResourceReleaseCollisionAndParentCloseAreDistinct(t *testing.T) {
	t.Run("collision", func(t *testing.T) {
		f := newRentalReplayFixture(t)
		_, a := reserveRentalAllocation(t, f)
		before := allocationSnapshot(t, f, a.ID)
		_, err := f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_resource_allocation_events(allocation_id,event_type,old_status,new_status,details) VALUES($1,'released','reserved','released','{"reason":"original decision"}')`, a.ID)
		require.NoError(t, err)
		require.ErrorContains(t, outbound_repo.NewOutboundRepo().ReleaseResourceAllocation(f.Ctx, a.ID, "another decision"), "original reason")
		require.Equal(t, before, allocationSnapshot(t, f, a.ID))
	})
	t.Run("parent close", func(t *testing.T) {
		f := newRentalReplayFixture(t)
		_, a := reserveRentalAllocation(t, f)
		require.NoError(t, f.close("0"))
		before := allocationSnapshot(t, f, a.ID)
		require.NoError(t, outbound_repo.NewOutboundRepo().ReleaseResourceAllocation(f.Ctx, a.ID, "late cleanup"))
		require.Equal(t, before, allocationSnapshot(t, f, a.ID))
		var count int
		require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT count(*) FROM reg_crypto_resource_allocation_events WHERE allocation_id=$1 AND event_type='released'`, a.ID).Scan(&count))
		require.Zero(t, count, "parent close must not acquire an invented allocation reason")
	})
}

func TestResourceReleaseLocksParentBeforeAllocation(t *testing.T) {
	f := newRentalReplayFixture(t)
	_, a := reserveRentalAllocation(t, f)
	tm := postgres.MustGetTxManager(f.Ctx)
	f.AssertLockOrder(t, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM doc_crypto_resource_rentals WHERE id=$1 FOR UPDATE`, f.rental)
		return err
	},
		func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM reg_crypto_resource_allocations WHERE id=$1 FOR UPDATE`, a.ID)
			return err
		},
		func(ctx context.Context) error {
			return outbound_repo.NewOutboundRepo().ReleaseResourceAllocation(ctx, a.ID, "release reservation")
		})
}
