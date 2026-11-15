//go:build integration

package crypto_worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/internal/domain/registers/cryptoposting"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"

	"github.com/stretchr/testify/require"
)

func TestWithdrawalSelectorSkipsBusyWalletWithoutReservingIt(t *testing.T) {
	f := newCustodyFixture(t)
	busy := f.wallet(t, &f.a, "liquidity_hot", "free", "200")
	available := f.wallet(t, &f.a, "liquidity_hot", "free", "100")
	f.enforce(t)
	holder, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(context.Background()) }()
	_, err = holder.Exec(f.ctx, "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE", busy)
	require.NoError(t, err)
	request := f.request(t, f.a, "70")
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	selected, err := NewOutboundRepo().ReserveHotWalletForWithdrawal(ctx, request, f.a, f.network, f.token, types.MustCryptoAmount("70"))
	require.NoError(t, err)
	require.Equal(t, available, selected.ID)
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_balance_reservations WHERE document_id=$1 AND wallet_id=$2", request, busy).Scan(&count))
	require.Zero(t, count)
}

func TestSweepSelectorSkipsBusyCollectorWithoutClaimingItsSource(t *testing.T) {
	f := newCustodyFixture(t)
	first := f.wallet(t, &f.a, "merchant_pool", "sweep_pending", "100")
	busy := f.wallet(t, &f.a, "liquidity_hot", "free", "0")
	second := f.wallet(t, &f.b, "merchant_pool", "sweep_pending", "100")
	collector := f.wallet(t, &f.b, "liquidity_hot", "free", "0")
	f.enforce(t)
	holder, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(context.Background()) }()
	_, err = holder.Exec(f.ctx, "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE", busy)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	tasks, err := NewOutboundRepo().ClaimSweepPendingWallets(ctx, 1)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, second, tasks[0].WalletID)
	require.Equal(t, collector, tasks[0].HotWalletID)
	var status string
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT status FROM cat_wallets WHERE id=$1", first).Scan(&status))
	require.Equal(t, "sweep_pending", status)
	_, err = f.pool.Exec(f.ctx, "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE NOWAIT", first)
	require.NoError(t, err, "skipped footprint must leave neither its source wallet nor its row status claimed")
}

func assertWorkerResourceOrder(t *testing.T, ctx context.Context, first, next, writer func(context.Context) error) {
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
			if err := tm.GetQuerier(ctx).QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
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
	go func() {
		writerDone <- tm.RunInTransaction(ctx, func(ctx context.Context) error {
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
		err := tm.GetQuerier(ctx).QueryRow(ctx, "SELECT $1::int=ANY(pg_blocking_pids($2::int))", controlPID, writerPID).Scan(&blocked)
		return err == nil && blocked
	}, 4*time.Second, 10*time.Millisecond)
	resume()
	require.NoError(t, <-controlDone)
	require.NoError(t, <-writerDone)
}

func TestWithdrawalReservationReplayLocksWalletBeforeRequest(t *testing.T) {
	f := newCustodyFixture(t)
	wallet := f.wallet(t, &f.a, "liquidity_hot", "free", "100")
	request := f.request(t, f.a, "70")
	repo := NewOutboundRepo()
	_, err := repo.ReserveHotWalletForWithdrawal(f.ctx, request, f.a, f.network, f.token, types.MustCryptoAmount("70"))
	require.NoError(t, err)
	assertWorkerResourceOrder(t, f.ctx,
		func(ctx context.Context) error {
			return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, wallet)
		},
		func(ctx context.Context) error {
			_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, "SELECT id FROM doc_withdrawal_requests WHERE id=$1 FOR UPDATE", request)
			return err
		},
		func(ctx context.Context) error {
			_, err := repo.ReserveHotWalletForWithdrawal(ctx, request, f.a, f.network, f.token, types.MustCryptoAmount("70"))
			return err
		})
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_balance_reservations WHERE document_id=$1", request).Scan(&count))
	require.Equal(t, 1, count)
}

func TestActivationFinalizationLocksTargetBeforeWorkflowRow(t *testing.T) {
	for _, scenario := range []string{"target_wallet", "operation_advisory"} {
		t.Run(scenario, func(t *testing.T) {
			f := newNetworkFeeFixture(t, crypto.SigningPurposeActivation)
			f.receipt.Status = crypto.ReceiptReverted
			f.prove(t)
			q := f.tm.GetQuerier(f.ctx)
			target := id.New()
			_, err := q.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,tier,wallet_role,chain_activation_status)
		VALUES($1,$1::uuid::text,'Activation target',$2,'0x00000000000000000000000000000000000000cd','target-key','pool','merchant_pool','activating')`, target, f.reservation.NetworkID)
			require.NoError(t, err)
			_, err = q.Exec(f.ctx, `INSERT INTO reg_crypto_account_activations(id,wallet_id,network_id,status) VALUES($1,$2,$3,'signing')`, f.reservation.BasisID, target, f.reservation.NetworkID)
			require.NoError(t, err)
			engine := posting.NewEngine(postgres.NewDocLocker())
			engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
			engine.AddVisitor(&cryptoposting.CryptoPlatformCostVisitor{})
			engine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(crypto_register_repo.NewCryptoBalanceRepo())))
			engine.AddRecorder(cryptoposting.NewCryptoPlatformCostRecorder(crypto_platform_cost.NewService(crypto_register_repo.NewCryptoPlatformCostRepo())))
			repo := document_repo.NewCryptoNetworkOperationRepo()
			service := crypto_network_operation.NewService(repo, engine, fixedNetworkOperationNumerator{}, nil)
			coordinator := NewCryptoNetworkOperationCoordinator(repo, service, NewFeeBudgetManager())
			doc, err := coordinator.EnsureActivationBroadcast(f.ctx, AccountActivationTask{ActivationID: f.reservation.BasisID, WalletID: target, NetworkID: f.reservation.NetworkID,
				Address: "0x00000000000000000000000000000000000000cd", HotWallet: HotWallet{ID: f.reservation.PayerWalletID}, CostTokenID: f.reservation.NativeTokenID}, f.receipt.TxHash)
			require.NoError(t, err)
			assertWorkerResourceOrder(t, f.ctx,
				func(ctx context.Context) error {
					if scenario == "operation_advisory" {
						return postgres.NewDocLocker().LockDocument(ctx, crypto_network_operation.DocumentType, doc)
					}
					return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, target)
				},
				func(ctx context.Context) error {
					if scenario == "operation_advisory" {
						return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, f.reservation.PayerWalletID, target)
					}
					_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, "SELECT id FROM reg_crypto_account_activations WHERE id=$1 FOR UPDATE", f.reservation.BasisID)
					return err
				},
				func(ctx context.Context) error {
					_, err := coordinator.FinalizeActivation(ctx, f.reservation.BasisID, f.reservation.ID, f.receipt)
					return err
				})
			var status string
			var owners int
			require.NoError(t, q.QueryRow(f.ctx, "SELECT chain_activation_status FROM cat_wallets WHERE id=$1", target).Scan(&status))
			require.Equal(t, "failed", status)
			require.NoError(t, q.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_network_fee_bindings WHERE reservation_id=$1 AND recorder_id=$2", f.reservation.ID, doc).Scan(&owners))
			require.Equal(t, 1, owners)
		})
	}
}
