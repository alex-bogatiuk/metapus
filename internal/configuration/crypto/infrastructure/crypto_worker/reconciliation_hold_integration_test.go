//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/token"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/logger"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

func seedBalanceReconciliationRun(t *testing.T, ctx context.Context, txm *sdktest.TxManager) id.ID {
	t.Helper()
	runID := id.New()
	_, err := txm.GetQuerier(ctx).Exec(ctx, `
		INSERT INTO reg_crypto_reconciliation_runs(id,status,completed_at)
		VALUES($1,'completed',NOW())
	`, runID)
	require.NoError(t, err)
	return runID
}

func balanceObservation(t *testing.T, ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) crypto.BalanceObservation {
	t.Helper()
	return crypto.BalanceObservation{RunID: seedBalanceReconciliationRun(t, ctx, txm), NetworkID: f.network,
		WalletID: f.wallet, TokenID: f.token, Kind: crypto.BalanceMismatchToken,
		LedgerAmount: types.NewRawAmountFromInt64(100), ChainAmount: types.NewRawAmountFromInt64(90)}
}

func TestReconciliationHoldConcurrentReplayAndResolution(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
		service := newBalanceReconciliationService()
		first := balanceObservation(t, ctx, txm, f)
		concurrent := func(observations []crypto.BalanceObservation) (created, updated, resolved int) {
			t.Helper()
			start := make(chan struct{})
			errs := make(chan error, len(observations))
			results := make(chan cryptoapp.BalanceReconciliationOutcome, len(observations))
			var wg sync.WaitGroup
			for _, o := range observations {
				wg.Go(func() {
					<-start
					result, err := service.Record(ctx, o)
					results <- result
					errs <- err
				})
			}
			close(start)
			wg.Wait()
			close(errs)
			close(results)
			for err := range errs {
				require.NoError(t, err)
			}
			for r := range results {
				if r.Created {
					created++
				}
				if r.Updated {
					updated++
				}
				if r.Resolved {
					resolved++
				}
			}
			return
		}
		repeated := make([]crypto.BalanceObservation, 20)
		for i := range repeated {
			repeated[i] = first
		}
		created, updated, resolved := concurrent(repeated)
		require.Equal(t, 1, created)
		require.Zero(t, updated+resolved)
		var firstHold, afterObservation string
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT to_jsonb(h)::text FROM reg_crypto_wallet_allocation_holds h WHERE wallet_id=$1 AND owner_type='reconciliation_incident'`, f.wallet).Scan(&firstHold))
		second := first
		second.RunID, second.ChainAmount = seedBalanceReconciliationRun(t, ctx, txm), types.NewRawAmountFromInt64(80)
		for i := range repeated {
			repeated[i] = second
		}
		created, updated, resolved = concurrent(repeated)
		require.Equal(t, 1, updated)
		require.Zero(t, created+resolved)
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT to_jsonb(h)::text FROM reg_crypto_wallet_allocation_holds h WHERE wallet_id=$1 AND owner_type='reconciliation_incident'`, f.wallet).Scan(&afterObservation))
		require.Equal(t, firstHold, afterObservation, "new balance observations preserve original hold evidence")
		matched := second
		matched.RunID, matched.ChainAmount = seedBalanceReconciliationRun(t, ctx, txm), matched.LedgerAmount
		for i := range repeated {
			if i%2 == 0 {
				repeated[i] = matched
			} else {
				repeated[i] = first
			}
		}
		created, updated, resolved = concurrent(repeated)
		require.Equal(t, 1, resolved)
		require.Zero(t, created+updated, "an old run cannot resurrect a resolved incident")
		q := txm.GetQuerier(ctx)
		var checks, observations, holds, occurrences int
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_reconciliation_checks WHERE wallet_id=$1`, f.wallet).Scan(&checks))
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_reconciliation_observations WHERE wallet_id=$1`, f.wallet).Scan(&observations))
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND status='resolved'`, f.wallet).Scan(&holds))
		require.NoError(t, q.QueryRow(ctx, `SELECT occurrence_count FROM reg_crypto_reconciliation_items WHERE wallet_id=$1`, f.wallet).Scan(&occurrences))
		require.Equal(t, 3, checks)
		require.Equal(t, 3, observations)
		require.Equal(t, 3, occurrences)
		require.Equal(t, 1, holds)
		first.RunID = seedBalanceReconciliationRun(t, ctx, txm)
		outcome, err := service.Record(ctx, first)
		require.NoError(t, err)
		require.True(t, outcome.Created, "a new mismatch gets a new owner, not a reopened hold")
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1`, f.wallet).Scan(&holds))
		require.Equal(t, 2, holds)
	})
}

func TestReconciliationHoldSelectiveMultiTokenResolution(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
		service := newBalanceReconciliationService()
		holds := cryptoapp.NewOperationalHoldService(contextTxManager{}, crypto_repo.NewOperationalHoldRepo())
		for _, owner := range []string{"wallet_reconciliation", "external_outflow"} {
			reason := "chain_balance_check_pending"
			if owner == "external_outflow" {
				reason = owner
			}
			require.NoError(t, holds.Acquire(ctx, cryptoapp.AcquireOperationalHoldCommand{Scope: cryptoapp.OperationalHoldScopeWallet,
				ScopeID: f.wallet, OwnerType: owner, OwnerID: f.wallet.String(), Reason: reason, HeldBy: "test"}))
		}
		tokenRead := balanceObservation(t, ctx, txm, f)
		nativeRead := tokenRead
		nativeRead.TokenID, nativeRead.Kind = f.native, crypto.BalanceMismatchNative
		_, err := service.Record(ctx, tokenRead)
		require.NoError(t, err)
		_, err = service.Record(ctx, nativeRead)
		require.NoError(t, err)
		countOpen := func(want int) {
			t.Helper()
			var count int
			require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND status='open'`, f.wallet).Scan(&count))
			require.Equal(t, want, count)
		}
		countOpen(4)
		tokenRead.RunID, tokenRead.ChainAmount = seedBalanceReconciliationRun(t, ctx, txm), tokenRead.LedgerAmount
		_, err = service.Record(ctx, tokenRead)
		require.NoError(t, err)
		require.NoError(t, service.CompleteWalletCheck(ctx, f.wallet))
		countOpen(3)
		nativeRead.RunID, nativeRead.ChainAmount = tokenRead.RunID, nativeRead.LedgerAmount
		_, err = service.Record(ctx, nativeRead)
		require.NoError(t, err)
		require.NoError(t, service.CompleteWalletCheck(ctx, f.wallet))
		require.NoError(t, service.CompleteWalletCheck(ctx, f.wallet))
		countOpen(1)
		var owner string
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT owner_type FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND status='open'`, f.wallet).Scan(&owner))
		require.Equal(t, "external_outflow", owner)
	})
}

type failingReconciliationHoldRepo struct {
	*crypto_repo.OperationalHoldRepo
	failAcquire, failResolve bool
}

var errInjectedReconciliation = errors.New("injected reconciliation persistence failure")

func (r failingReconciliationHoldRepo) AcquireOperationalHold(ctx context.Context, command cryptoapp.AcquireOperationalHoldCommand) error {
	if err := r.OperationalHoldRepo.AcquireOperationalHold(ctx, command); err != nil {
		return err
	}
	if r.failAcquire {
		return errInjectedReconciliation
	}
	return nil
}

func (r failingReconciliationHoldRepo) ResolveOperationalHolds(ctx context.Context, ownerType, ownerID, actor, reason string) (int64, error) {
	count, err := r.OperationalHoldRepo.ResolveOperationalHolds(ctx, ownerType, ownerID, actor, reason)
	if err == nil && r.failResolve {
		return count, errInjectedReconciliation
	}
	return count, err
}

func reconciliationServiceWithFailure(acquire, resolve bool) *cryptoapp.BalanceReconciliationService {
	txm := contextTxManager{}
	holds := cryptoapp.NewOperationalHoldService(txm, failingReconciliationHoldRepo{crypto_repo.NewOperationalHoldRepo(), acquire, resolve})
	return cryptoapp.NewBalanceReconciliationService(txm, crypto_repo.NewBalanceReconciliationRepo(), holds)
}

func TestReconciliationHoldAtomicRollbackAndRetry(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
		o := balanceObservation(t, ctx, txm, f)
		outcome, err := reconciliationServiceWithFailure(true, false).Record(ctx, o)
		require.ErrorIs(t, err, errInjectedReconciliation)
		require.Equal(t, cryptoapp.BalanceReconciliationOutcome{}, outcome)
		for _, table := range []string{"reg_crypto_reconciliation_checks", "reg_crypto_reconciliation_items", "reg_crypto_reconciliation_observations", "reg_crypto_wallet_allocation_holds"} {
			var count int
			require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE wallet_id=$1", f.wallet).Scan(&count))
			require.Zero(t, count, table)
		}
		service := newBalanceReconciliationService()
		outcome, err = service.Record(ctx, o)
		require.NoError(t, err)
		require.True(t, outcome.Created)
		o.RunID, o.ChainAmount = seedBalanceReconciliationRun(t, ctx, txm), o.LedgerAmount
		outcome, err = reconciliationServiceWithFailure(false, true).Record(ctx, o)
		require.ErrorIs(t, err, errInjectedReconciliation)
		require.Equal(t, cryptoapp.BalanceReconciliationOutcome{}, outcome)
		var state string
		var count int
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT status FROM reg_crypto_reconciliation_items WHERE wallet_id=$1`, f.wallet).Scan(&state))
		require.Equal(t, "open", state)
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_reconciliation_checks WHERE wallet_id=$1`, f.wallet).Scan(&count))
		require.Equal(t, 1, count)
		outcome, err = service.Record(ctx, o)
		require.NoError(t, err)
		require.True(t, outcome.Resolved)
	})
}

func TestReconciliationHoldRejectsConflictingReplay(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
		o := balanceObservation(t, ctx, txm, f)
		service := newBalanceReconciliationService()
		_, err := service.Record(ctx, o)
		require.NoError(t, err)
		o.ChainAmount = o.LedgerAmount
		_, err = service.Record(ctx, o)
		require.ErrorContains(t, err, "different balance evidence")
		var state string
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT status FROM reg_crypto_reconciliation_items WHERE wallet_id=$1`, f.wallet).Scan(&state))
		require.Equal(t, "open", state)
	})
}

type reconciliationBalanceDriver struct {
	fundingReceiptDriver
	nativeAmount, tokenAmount types.RawAmount
	readErr                   error
}

func (d reconciliationBalanceDriver) Balances() crypto.BalanceReader { return d }
func (d reconciliationBalanceDriver) NativeBalance(ctx context.Context, _ string) (types.RawAmount, error) {
	if postgres.MustGetTxManager(ctx).GetTx(ctx) != nil {
		return types.ZeroRawAmount(), errors.New("RPC inside transaction")
	}
	return d.nativeAmount, d.readErr
}
func (d reconciliationBalanceDriver) TokenBalance(ctx context.Context, _ crypto.AssetRef, _ string) (types.RawAmount, error) {
	if postgres.MustGetTxManager(ctx).GetTx(ctx) != nil {
		return types.ZeroRawAmount(), errors.New("RPC inside transaction")
	}
	return d.tokenAmount, d.readErr
}

func TestReconciliationHoldWorkerFailureIsNotSuccessfulRun(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
		registry := crypto.NewChainDriverRegistry()
		driver := reconciliationBalanceDriver{fundingReceiptDriver: fundingReceiptDriver{network: crypto.NetworkRef{
			ID: f.network, Code: "TEST", Family: crypto.ChainFamilyTRON, NetworkIdentifier: "nile",
			NativeTokenID: f.native, NativeDecimals: 6, RequiredConfirmations: 12, FinalityMode: crypto.FinalityByConfirmations}},
			nativeAmount: types.NewRawAmountFromInt64(199999999), tokenAmount: types.NewRawAmountFromInt64(999999)}
		require.NoError(t, registry.Register(driver))
		p := &CryptoProcessor{drivers: registry, log: logger.Default(), workerID: "reconciliation-test",
			balanceReconciler: reconciliationServiceWithFailure(true, false)}
		result, err := p.RunWalletBalanceReconciliation(ctx, f.wallet)
		require.ErrorIs(t, err, errInjectedReconciliation)
		require.True(t, result.Acquired)
		var state string
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT status FROM reg_crypto_reconciliation_runs WHERE id=$1`, result.RunID).Scan(&state))
		require.Equal(t, "failed", state)
		p.balanceReconciler = newBalanceReconciliationService()
		result, err = p.RunWalletBalanceReconciliation(ctx, f.wallet)
		require.NoError(t, err)
		require.Equal(t, 2, result.ItemsCreated)
		result, err = p.RunWalletBalanceReconciliation(ctx, f.wallet)
		require.NoError(t, err)
		require.Equal(t, 2, result.ItemsUpdated)
		// RPC outage neither fabricates a zero balance nor resolves the holds.
		driver.readErr = errors.New("test RPC unavailable")
		p.drivers = crypto.NewChainDriverRegistry()
		require.NoError(t, p.drivers.Register(driver))
		result, err = p.RunWalletBalanceReconciliation(ctx, f.wallet)
		require.NoError(t, err)
		require.Equal(t, 2, result.ReadErrors)
		require.Zero(t, result.ItemsResolved)
		var holds int
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND status='open'`, f.wallet).Scan(&holds))
		require.Equal(t, 2, holds)
	})
}

func TestReconciliationHoldRepeatedMismatchPreservesAcquisition(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, txm *sdktest.TxManager, f sweepCoordinationFixture) {
		service := newBalanceReconciliationService()
		w := reconciliationWalletBalance{WalletID: f.wallet, NetworkID: f.network, TokenID: f.token,
			TokenStandard: token.TokenStandardTRC20, LedgerAmount: types.NewRawAmountFromInt64(100)}
		first := seedBalanceReconciliationRun(t, ctx, txm)
		observation := crypto.BalanceObservation{RunID: first, WalletID: w.WalletID, NetworkID: w.NetworkID,
			TokenID: w.TokenID, Kind: reconciliationKind(w.TokenStandard), LedgerAmount: w.LedgerAmount,
			ChainAmount: types.NewRawAmountFromInt64(90)}
		outcome, err := service.Record(ctx, observation)
		require.NoError(t, err)
		require.True(t, outcome.Created)
		var acquisition string
		q := txm.GetQuerier(ctx)
		require.NoError(t, q.QueryRow(ctx, `SELECT row_to_json(h)::text FROM reg_crypto_wallet_allocation_holds h WHERE wallet_id=$1`, f.wallet).Scan(&acquisition))
		second := seedBalanceReconciliationRun(t, ctx, txm)
		observation.RunID, observation.ChainAmount = second, types.NewRawAmountFromInt64(80)
		outcome, err = service.Record(ctx, observation)
		require.NoError(t, err, "a new observation must not overwrite hold acquisition details")
		require.True(t, outcome.Updated)
		var after string
		require.NoError(t, q.QueryRow(ctx, `SELECT row_to_json(h)::text FROM reg_crypto_wallet_allocation_holds h WHERE wallet_id=$1`, f.wallet).Scan(&after))
		require.JSONEq(t, acquisition, after)
		var incidents, observations, occurrences int
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*),MAX(occurrence_count) FROM reg_crypto_reconciliation_items WHERE wallet_id=$1`, f.wallet).Scan(&incidents, &occurrences))
		require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_reconciliation_observations WHERE wallet_id=$1`, f.wallet).Scan(&observations))
		require.Equal(t, 1, incidents)
		require.Equal(t, 2, occurrences)
		require.Equal(t, 2, observations)
	})
}
