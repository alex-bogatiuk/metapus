//go:build integration

package crypto_repo_test

import (
	"context"
	"sync"
	"testing"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/outbound_repo"

	"github.com/stretchr/testify/require"
)

func claimRental(t *testing.T, f rentalReplayFixture) outboundapp.ResourceRentalClaim {
	t.Helper()
	rentals, err := outbound_repo.NewOutboundRepo().ClaimResourceRentalsForMaintenance(f.Ctx, "same-worker", 1)
	require.NoError(t, err)
	require.Len(t, rentals, 1)
	require.Equal(t, f.rental, rentals[0].ID)
	claim := rentals[0].MaintenanceClaim()
	require.False(t, id.IsNil(claim.Token))
	return claim
}
func expiredRental(t *testing.T) rentalReplayFixture {
	t.Helper()
	f := newRentalReplayFixture(t)
	require.NoError(t, f.activate("1000"))
	_, err := f.Pool.Exec(f.Ctx, `UPDATE doc_crypto_resource_rentals SET expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, f.rental)
	require.NoError(t, err)
	return f
}
func expireRentalClaim(t *testing.T, f rentalReplayFixture) {
	t.Helper()
	_, err := f.Pool.Exec(f.Ctx, `UPDATE doc_crypto_resource_rentals SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.rental)
	require.NoError(t, err)
}
func rejectRentalClaim(t *testing.T, f rentalReplayFixture, c outboundapp.ResourceRentalClaim) {
	t.Helper()
	r := outbound_repo.NewOutboundRepo()
	before, amounts := f.snapshot(t), f.amounts(t)
	for _, run := range []func() error{
		func() error { return r.RenewResourceRentalClaim(f.Ctx, c) },
		func() error { return r.ReleaseResourceRentalClaim(f.Ctx, c) },
		func() error { return r.MarkResourceRentalClaimReclaiming(f.Ctx, c) },
		func() error { return r.MarkResourceRentalClaimHeld(f.Ctx, c, "stale result", nil) },
		func() error {
			return r.CloseResourceRentalClaim(f.Ctx, c, types.MustCryptoAmount("200"), nil, nil, "closed")
		},
	} {
		require.ErrorContains(t, run(), "claim is missing or stale")
		require.Equal(t, before, f.snapshot(t))
		require.Equal(t, amounts, f.amounts(t))
	}
}

func TestRentalMaintenanceClaimFencesOldWorkerAndUnclaimedRecovery(t *testing.T) {
	f := expiredRental(t)
	r := outbound_repo.NewOutboundRepo()
	first := claimRental(t, f)
	before := f.snapshot(t)
	rentals, err := r.ClaimResourceRentalsForMaintenance(f.Ctx, "other-worker", 10)
	require.NoError(t, err)
	require.Empty(t, rentals)
	require.Equal(t, before, f.snapshot(t))
	rejectRentalClaim(t, f, outboundapp.ResourceRentalClaim{RentalID: f.rental, Token: id.New()})
	rejectRentalClaim(t, f, outboundapp.ResourceRentalClaim{RentalID: f.rental})
	for _, run := range []func() error{
		func() error { return r.MarkResourceRentalHeld(f.Ctx, f.rental, "unowned result", nil) },
		func() error { return r.MarkResourceRentalReclaiming(f.Ctx, f.rental) },
		func() error { return f.close("200") },
		func() error { return f.activate("1000") },
	} {
		require.ErrorContains(t, run(), "owned by a maintenance claim")
		require.Equal(t, before, f.snapshot(t))
	}
	expireRentalClaim(t, f)
	rejectRentalClaim(t, f, first)
	next := claimRental(t, f)
	require.NotEqual(t, first.Token, next.Token)
	rejectRentalClaim(t, f, first)
	require.NoError(t, r.RenewResourceRentalClaim(f.Ctx, next))
	require.NoError(t, r.MarkResourceRentalClaimReclaiming(f.Ctx, next))
	require.NoError(t, r.CloseResourceRentalClaim(f.Ctx, next, types.MustCryptoAmount("200"), nil, nil, "closed"))
	require.Equal(t, []string{"200", "-800", "800"}, f.amounts(t))
	rejectRentalClaim(t, f, next)
	terminal := f.snapshot(t)
	require.NoError(t, f.close("200"))
	require.Equal(t, terminal, f.snapshot(t))
}

func TestRentalMaintenanceClaimConcurrentAcquisitionAndRelease(t *testing.T) {
	f := expiredRental(t)
	r := outbound_repo.NewOutboundRepo()
	var wg sync.WaitGroup
	results := make(chan []*outbound_repo.ResourceRentalRecord, 4)
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			rows, err := r.ClaimResourceRentalsForMaintenance(f.Ctx, "shared-worker", 1)
			results <- rows
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var claims []outboundapp.ResourceRentalClaim
	for rows := range results {
		for _, row := range rows {
			claims = append(claims, row.MaintenanceClaim())
		}
	}
	require.Len(t, claims, 1)
	require.NoError(t, r.MarkResourceRentalClaimHeld(f.Ctx, claims[0], "provider timeout", nil))
	rows, err := r.ClaimResourceRentalsForMaintenance(f.Ctx, "retry-too-early", 1)
	require.NoError(t, err)
	require.Empty(t, rows)
	expireRentalClaim(t, f)
	next := claimRental(t, f)
	require.NotEqual(t, claims[0].Token, next.Token)
	rejectRentalClaim(t, f, claims[0])
	require.NoError(t, r.ReleaseResourceRentalClaim(f.Ctx, next))
	rejectRentalClaim(t, f, next)
	last := claimRental(t, f)
	require.NotEqual(t, next.Token, last.Token)
}

func TestRentalMaintenanceClaimUsesWallTimeAndReadCommitted(t *testing.T) {
	f := expiredRental(t)
	c := claimRental(t, f)
	r := outbound_repo.NewOutboundRepo()
	tm := postgres.MustGetTxManager(f.Ctx)
	require.NoError(t, tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		var started string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT now()::text`).Scan(&started))
		_, err := f.Pool.Exec(f.Ctx, `UPDATE doc_crypto_resource_rentals SET lease_expires_at=clock_timestamp()-interval '1 microsecond' WHERE id=$1`, f.rental)
		require.NoError(t, err)
		var oldClockWouldAccept bool
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT lease_expires_at>now() AND lease_expires_at<clock_timestamp() FROM doc_crypto_resource_rentals WHERE id=$1`, f.rental).Scan(&oldClockWouldAccept))
		require.True(t, oldClockWouldAccept)
		require.ErrorContains(t, r.RenewResourceRentalClaim(ctx, c), "claim is missing or stale")
		return nil
	}))
	require.NoError(t, tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`)
		require.NoError(t, err)
		_, err = r.ClaimResourceRentalsForMaintenance(ctx, "stale", 1)
		require.ErrorContains(t, err, "read committed")
		require.ErrorContains(t, r.RenewResourceRentalClaim(ctx, c), "read committed")
		return nil
	}))
}

func TestRentalMaintenanceClaimLocksAccountBeforeRental(t *testing.T) {
	f := expiredRental(t)
	c := claimRental(t, f)
	r := outbound_repo.NewOutboundRepo()
	tm := postgres.MustGetTxManager(f.Ctx)
	f.AssertLockOrder(t, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM cat_crypto_provider_accounts WHERE id=$1 FOR UPDATE`, f.account)
		return err
	}, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM doc_crypto_resource_rentals WHERE id=$1 FOR UPDATE`, f.rental)
		return err
	}, func(ctx context.Context) error { return r.MarkResourceRentalClaimReclaiming(ctx, c) })
}
