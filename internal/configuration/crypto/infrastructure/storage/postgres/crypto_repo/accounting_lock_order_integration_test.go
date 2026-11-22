//go:build integration

package crypto_repo

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/sdktest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// Wait for a real PostgreSQL wait edge, then let its owner acquire the next
// resource. This exercises both orders without scheduling sleeps or retries.
func assertAccountingLockOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	first, next, writer func(context.Context) error, writerOptions ...sdktest.TxOptions) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	tm := postgres.MustGetTxManager(ctx)
	ready, started := make(chan int, 1), make(chan int, 1)
	controlDone, writerDone := make(chan error, 1), make(chan error, 1)
	proceed := make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(proceed) }) }
	defer resume()
	go func() {
		controlDone <- tm.RunInTransaction(ctx, func(ctx context.Context) error {
			var pid int
			q := tm.GetQuerier(ctx)
			if err := q.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				return err
			}
			if err := first(ctx); err != nil {
				ready <- 0
				return err
			}
			ready <- pid
			select {
			case <-proceed:
			case <-ctx.Done():
				return ctx.Err()
			}
			bounded, stop := context.WithTimeout(ctx, 2*time.Second)
			defer stop()
			return next(bounded)
		})
	}()
	controlPID := <-ready
	require.NotZero(t, controlPID)
	runWriter := tm.RunInTransaction
	if len(writerOptions) > 0 {
		runWriter = func(ctx context.Context, fn func(context.Context) error) error {
			return tm.RunInTransactionWithOptions(ctx, writerOptions[0], fn)
		}
	}
	go func() {
		writerDone <- runWriter(ctx, func(ctx context.Context) error {
			var pid int
			if err := tm.GetQuerier(ctx).QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				return err
			}
			started <- pid
			return writer(ctx)
		})
	}()
	writerPID := <-started
	require.Eventually(t, func() bool {
		var blocked bool
		err := pool.QueryRow(ctx, "SELECT $1::int=ANY(pg_blocking_pids($2::int))", controlPID, writerPID).Scan(&blocked)
		return err == nil && blocked
	}, 4*time.Second, 10*time.Millisecond, "writer must wait before taking the next resource")
	resume()
	require.NoError(t, <-controlDone, "resource owner must advance without deadlock")
	require.NoError(t, <-writerDone)
}

func TestAccountingLockOrderProofWalletBeforeBalance(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	proof := f.proof
	proof.Event.Confirmations++
	for i := range proof.Sources {
		proof.Sources[i].HeadHeight++
	}
	assertAccountingLockOrder(t, f.ctx, f.pool,
		func(ctx context.Context) error {
			return NewCustodyRecognitionRepo().LockWallets(ctx, f.request.Basis.WalletID)
		},
		func(ctx context.Context) error {
			_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, "SELECT fn_crypto_lock_receipt_balance($1,$2)", f.request.Basis.WalletID, f.request.Basis.TokenID)
			return err
		},
		func(ctx context.Context) error {
			_, err := NewBlockIngestionRepo().SaveVerification(ctx, proof)
			return err
		})
}

func TestAccountingLockOrderCaptureWalletBeforeCheckpoint(t *testing.T) {
	f := newReceiptIntakeFixture(t, nil)
	network, wallet := f.proof.Event.NetworkID, f.request.Basis.WalletID
	baseline := crypto.WatcherBlockFingerprint{BlockNumber: 10, BlockHash: fmt.Sprintf("%064x", 10), Timestamp: 1750000000000}
	_, err := f.pool.Exec(f.ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version)
		VALUES($1,10,$2,$3,'shadow',2)`, network, baseline.BlockHash, baseline.Timestamp)
	require.NoError(t, err)
	state, err := NewWatcherStateRepo().Acquire(f.ctx, &crypto.WatcherState{NetworkID: network, WatcherKey: "lock-order:shadow", LastBlock: 10, Fingerprint: baseline.BlockHash}, "lock-order-test")
	require.NoError(t, err)
	event := f.proof.Event
	event.DecoderVersion, event.BlockNumber, event.BlockHash = 2, 11, fmt.Sprintf("%064x", 11)
	event.Timestamp = time.UnixMilli(baseline.Timestamp + 3000)
	candidate := *state
	candidate.LastBlock, candidate.LastTimestamp, candidate.Fingerprint = 11, event.Timestamp.UnixMilli(), event.BlockHash
	capture := crypto.CapturedBlock{DecoderVersion: 2, Shadow: true, Sequential: true, ParentHash: baseline.BlockHash, Events: []crypto.BlockchainEvent{event},
		Commit: crypto.WatcherBlockCommit{Candidate: candidate, ExpectedBlock: 10, ExpectedHash: baseline.BlockHash,
			Block: crypto.WatcherBlockFingerprint{BlockNumber: 11, BlockHash: event.BlockHash, Timestamp: candidate.LastTimestamp}}}
	assertAccountingLockOrder(t, f.ctx, f.pool,
		func(ctx context.Context) error { return NewCustodyRecognitionRepo().LockWallets(ctx, wallet) },
		func(ctx context.Context) error {
			_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, "SELECT decoder_version FROM sys_crypto_block_ingestion WHERE network_id=$1 FOR SHARE", network)
			return err
		},
		func(ctx context.Context) error { return NewBlockIngestionRepo().CaptureBlock(ctx, capture) })
	var height int64
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT last_block FROM sys_chain_watcher_state WHERE network_id=$1 AND watcher_key=$2", network, state.WatcherKey).Scan(&height))
	require.Equal(t, int64(11), height)
}

func TestAccountingLockOrderBatchWalletsAndScopesAreDeterministic(t *testing.T) {
	f := newAMLRoutingFixture(t, false, crypto.ChainFamilyEVM)
	second := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,custody_merchant_id)
		SELECT $1,$1::uuid::text,'Other wallet',network_id,$1::uuid::text,'lock-key',custody_merchant_id FROM cat_wallets WHERE id=$2`, second, f.input.WalletID)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := range 10 {
		wg.Go(func() {
			keys := []id.ID{second, f.input.WalletID, second}
			if i%2 == 1 {
				keys = []id.ID{f.input.WalletID, second}
			}
			errs <- postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error { return NewCustodyRecognitionRepo().LockWallets(ctx, keys...) })
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func TestAccountingLockOrderWalletTakesWholeOwnerNetworkScope(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	var tokens []id.ID
	rows, err := f.pool.Query(f.ctx, `SELECT token_id FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 ORDER BY token_id`, f.input.Request.MerchantID, f.input.Request.NetworkID)
	require.NoError(t, err)
	for rows.Next() {
		var token id.ID
		require.NoError(t, rows.Scan(&token))
		tokens = append(tokens, token)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Len(t, tokens, 2)
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		if err := NewCustodyRecognitionRepo().LockWallet(ctx, target, f.input.Request.TokenID); err != nil {
			return err
		}
		for _, token := range tokens {
			_, probe := f.pool.Exec(ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3 FOR UPDATE NOWAIT`, f.input.Request.MerchantID, f.input.Request.NetworkID, token)
			require.ErrorContains(t, probe, "55P03", "operation and native scopes must both precede the wallet")
		}
		return nil
	})
	require.NoError(t, err)
}

func TestAccountingLockOrderRemainingScopeDoesNotHoldWallet(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	assertAccountingLockOrder(t, f.ctx, f.pool,
		func(ctx context.Context) error {
			_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3 FOR UPDATE`, f.input.Request.MerchantID, f.input.Request.NetworkID, f.input.Request.TokenID)
			return err
		},
		func(ctx context.Context) error {
			_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE", target)
			return err
		},
		func(ctx context.Context) error {
			return NewCustodyRecognitionRepo().LockWallet(ctx, target, f.input.Request.TokenID)
		})
}
