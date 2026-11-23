//go:build integration

package crypto_repo

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"

	"github.com/stretchr/testify/require"
)

func holdCommand(f ActivationSelectorFixture, scope cryptoapp.OperationalHoldScope, owner string) cryptoapp.AcquireOperationalHoldCommand {
	target := f.Network
	if scope == cryptoapp.OperationalHoldScopeWallet {
		target = f.A
	}
	return cryptoapp.AcquireOperationalHoldCommand{Scope: scope, ScopeID: target, Reason: "external_outflow", OwnerType: "test-incident", OwnerID: owner, HeldBy: "detector", Details: map[string]any{"source": "verified", "amount": "100"}}
}

func holdSnapshot(t *testing.T, f ActivationSelectorFixture) string {
	t.Helper()
	var snapshot string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT jsonb_build_object(
 'network',(SELECT COALESCE(jsonb_agg(to_jsonb(h) ORDER BY id),'[]') FROM reg_crypto_network_holds h WHERE network_id=$1),
 'wallet',(SELECT COALESCE(jsonb_agg(to_jsonb(h) ORDER BY id),'[]') FROM reg_crypto_wallet_allocation_holds h WHERE wallet_id IN ($2,$3,$4)))::text`, f.Network, f.A, f.B, f.Hot).Scan(&snapshot))
	return snapshot
}

func TestOperationalHoldAcquisitionChecksCompletePayload(t *testing.T) {
	for _, scope := range []cryptoapp.OperationalHoldScope{cryptoapp.OperationalHoldScopeNetwork, cryptoapp.OperationalHoldScopeWallet} {
		t.Run(string(scope), func(t *testing.T) {
			f := NewActivationSelectorFixture(t)
			r := NewOperationalHoldRepo()
			command := holdCommand(f, scope, id.New().String())
			require.NoError(t, r.AcquireOperationalHold(f.Ctx, command))
			first := holdSnapshot(t, f)
			command.Details = map[string]any{"amount": "100", "source": "verified"}
			require.NoError(t, r.AcquireOperationalHold(f.Ctx, command))
			require.Equal(t, first, holdSnapshot(t, f))
			for _, change := range []func(*cryptoapp.AcquireOperationalHoldCommand){
				func(c *cryptoapp.AcquireOperationalHoldCommand) { c.Reason = "manual" },
				func(c *cryptoapp.AcquireOperationalHoldCommand) { c.HeldBy = "another-detector" },
				func(c *cryptoapp.AcquireOperationalHoldCommand) {
					c.Details = map[string]any{"amount": "101", "source": "verified"}
				},
			} {
				altered := command
				change(&altered)
				require.ErrorContains(t, r.AcquireOperationalHold(f.Ctx, altered), "original acquisition")
				require.Equal(t, first, holdSnapshot(t, f))
			}
			count, err := r.ResolveOperationalHolds(f.Ctx, command.OwnerType, command.OwnerID, "reviewer", "verified")
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			resolved := holdSnapshot(t, f)
			require.NoError(t, r.AcquireOperationalHold(f.Ctx, command))
			require.Equal(t, resolved, holdSnapshot(t, f))
			changed := command
			changed.HeldBy = "changed-after-resolution"
			require.ErrorContains(t, r.AcquireOperationalHold(f.Ctx, changed), "original acquisition")
			require.Equal(t, resolved, holdSnapshot(t, f))
			later := command
			if scope == cryptoapp.OperationalHoldScopeNetwork {
				later.Scope = cryptoapp.OperationalHoldScopeWallet
				later.ScopeID = f.A
			} else {
				later.ScopeID = f.B
			}
			require.ErrorContains(t, r.AcquireOperationalHold(f.Ctx, later), "cannot acquire another target")
			require.Equal(t, resolved, holdSnapshot(t, f))
		})
	}
}

func TestOperationalHoldResolutionChecksEntireCohortBeforeMutation(t *testing.T) {
	f := NewActivationSelectorFixture(t)
	r := NewOperationalHoldRepo()
	owner := id.New().String()
	network, wallet := holdCommand(f, cryptoapp.OperationalHoldScopeNetwork, owner), holdCommand(f, cryptoapp.OperationalHoldScopeWallet, owner)
	require.NoError(t, r.AcquireOperationalHold(f.Ctx, network))
	require.NoError(t, r.AcquireOperationalHold(f.Ctx, wallet))
	// Existing mixed history: wallet already resolved, network remains open.
	_, err := f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_wallet_allocation_holds SET status='resolved',resolved_at=clock_timestamp(),resolved_by='reviewer',resolution='verified' WHERE owner_type=$1 AND owner_id=$2`, wallet.OwnerType, owner)
	require.NoError(t, err)
	before := holdSnapshot(t, f)
	for _, attempt := range [][2]string{{"other-reviewer", "verified"}, {"reviewer", "changed-reason"}} {
		count, err := r.ResolveOperationalHolds(f.Ctx, wallet.OwnerType, owner, attempt[0], attempt[1])
		require.ErrorContains(t, err, "original actor or reason")
		require.Zero(t, count)
		require.Equal(t, before, holdSnapshot(t, f))
	}
	count, err := r.ResolveOperationalHolds(f.Ctx, wallet.OwnerType, owner, "reviewer", "verified")
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	resolved := holdSnapshot(t, f)
	count, err = r.ResolveOperationalHolds(f.Ctx, wallet.OwnerType, owner, "reviewer", "verified")
	require.NoError(t, err)
	require.Zero(t, count)
	require.Equal(t, resolved, holdSnapshot(t, f))
}

func TestOperationalHoldResolutionLeavesOtherOwnersAndMoney(t *testing.T) {
	f := NewActivationSelectorFixture(t)
	r := NewOperationalHoldRepo()
	owner, other := id.New().String(), id.New().String()
	for _, scope := range []cryptoapp.OperationalHoldScope{cryptoapp.OperationalHoldScopeNetwork, cryptoapp.OperationalHoldScopeWallet} {
		require.NoError(t, r.AcquireOperationalHold(f.Ctx, holdCommand(f, scope, owner)))
		require.NoError(t, r.AcquireOperationalHold(f.Ctx, holdCommand(f, scope, other)))
	}
	count, err := r.ResolveOperationalHolds(f.Ctx, "test-incident", owner, "reviewer", "verified")
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	var otherOpen int
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT (SELECT count(*) FROM reg_crypto_network_holds WHERE owner_id=$1 AND status='open')+(SELECT count(*) FROM reg_crypto_wallet_allocation_holds WHERE owner_id=$1 AND status='open')`, other).Scan(&otherOpen))
	require.Equal(t, 2, otherOpen)
	var balance string
	var movements int
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT amount::text,(SELECT count(*) FROM reg_crypto_balance_movements WHERE wallet_id=$1) FROM reg_crypto_balance_balances WHERE wallet_id=$1 AND token_id=$2`, f.Hot, f.Native).Scan(&balance, &movements))
	require.Equal(t, "1000", balance)
	require.Zero(t, movements)
}

func TestOperationalHoldOwnerContentionIsNonblocking(t *testing.T) {
	f := NewActivationSelectorFixture(t)
	r := NewOperationalHoldRepo()
	command := holdCommand(f, cryptoapp.OperationalHoldScopeWallet, id.New().String())
	require.NoError(t, r.AcquireOperationalHold(f.Ctx, command))
	before := holdSnapshot(t, f)
	tm := postgres.MustGetTxManager(f.Ctx)
	ready, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(f.Ctx, 8*time.Second)
	defer cancel()
	go func() {
		done <- tm.RunInTransaction(ctx, func(ctx context.Context) error {
			if err := lockOperationalHoldOwner(ctx, command.OwnerType, command.OwnerID); err != nil {
				return err
			}
			close(ready)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	<-ready
	require.NoError(t, tm.RunInTransaction(ctx, func(ctx context.Context) error {
		// A caller already owns the wallet. It must not wait for the owner gate.
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.A)
		if err != nil {
			return err
		}
		require.ErrorContains(t, r.AcquireOperationalHold(ctx, command), "owner is busy")
		_, err = r.ResolveOperationalHolds(ctx, command.OwnerType, command.OwnerID, "reviewer", "verified")
		require.ErrorContains(t, err, "owner is busy")
		return nil
	}))
	require.Equal(t, before, holdSnapshot(t, f))
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, r.AcquireOperationalHold(f.Ctx, command))
}

func TestOperationalHoldConcurrentAcquireResolvePreservesOneHistory(t *testing.T) {
	f := NewActivationSelectorFixture(t)
	r := NewOperationalHoldRepo()
	command := holdCommand(f, cryptoapp.OperationalHoldScopeNetwork, id.New().String())
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Go(func() { errs <- r.AcquireOperationalHold(f.Ctx, command) })
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else {
			var app *apperror.AppError
			require.ErrorAs(t, err, &app)
			require.Equal(t, apperror.CodeConflict, app.Code)
		}
	}
	require.Positive(t, successes)
	require.NoError(t, r.AcquireOperationalHold(f.Ctx, command))
	var count int
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT count(*) FROM reg_crypto_network_holds WHERE owner_id=$1`, command.OwnerID).Scan(&count))
	require.Equal(t, 1, count)
	errs = make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			_, err := r.ResolveOperationalHolds(f.Ctx, command.OwnerType, command.OwnerID, "reviewer", "verified")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			var app *apperror.AppError
			require.ErrorAs(t, err, &app)
			require.Equal(t, apperror.CodeConflict, app.Code)
		}
	}
	_, err := r.ResolveOperationalHolds(f.Ctx, command.OwnerType, command.OwnerID, "reviewer", "verified")
	require.NoError(t, err)
	require.NoError(t, r.AcquireOperationalHold(f.Ctx, command))
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT count(*) FROM reg_crypto_network_holds WHERE owner_id=$1`, command.OwnerID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestOperationalHoldRequiresReadCommittedAndExplicitTargets(t *testing.T) {
	f := NewActivationSelectorFixture(t)
	r := NewOperationalHoldRepo()
	command := holdCommand(f, cryptoapp.OperationalHoldScopeWallet, id.New().String())
	require.NoError(t, r.AcquireOperationalHold(f.Ctx, command))
	before := holdSnapshot(t, f)
	require.NoError(t, postgres.MustGetTxManager(f.Ctx).RunInTransactionWithOptions(f.Ctx, postgres.SerializableTxOptions(), func(ctx context.Context) error {
		require.ErrorContains(t, r.AcquireOperationalHold(ctx, command), "read committed")
		_, err := r.ResolveOperationalHolds(ctx, command.OwnerType, command.OwnerID, "reviewer", "verified")
		require.ErrorContains(t, err, "read committed")
		return nil
	}))
	require.Equal(t, before, holdSnapshot(t, f))
	unknown := command
	unknown.ScopeID = id.New()
	require.Error(t, r.AcquireOperationalHold(f.Ctx, unknown))
	require.Equal(t, before, holdSnapshot(t, f))
	hot := command
	hot.ScopeID = f.Hot
	require.NoError(t, r.AcquireOperationalHold(f.Ctx, hot))
	require.Equal(t, before, holdSnapshot(t, f))
	_, err := r.ResolveOperationalHolds(f.Ctx, "test-incident", "missing", "reviewer", "verified")
	require.Error(t, err)
	require.Equal(t, before, holdSnapshot(t, f))
}
