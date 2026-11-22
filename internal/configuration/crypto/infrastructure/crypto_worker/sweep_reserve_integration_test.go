//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/application/outboundapp"
	"metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/tenant"
)

func TestQuerySweepCandidatesIncludesSettledPayments(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	errRollback := errors.New("rollback integration fixture")

	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		networkID, tokenID, merchantID := id.New(), id.New(), id.New()
		walletID, invoiceID, paymentID := id.New(), id.New(), id.New()
		suffix := id.Compact(networkID)
		now := time.Now().UTC()
		_, insertErr := q.Exec(txCtx, `
			INSERT INTO cat_blockchain_networks(
				id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,confirmations_needed)
			VALUES($1,$2,'Settled sweep candidate network','tron',$3,'TRX',6,1);
			INSERT INTO cat_merchants(id,code,name)
			VALUES($4,$5,'Settled sweep candidate merchant');
			INSERT INTO cat_tokens(
				id,code,name,network_id,contract_address,symbol,decimal_places,token_standard,sweep_threshold)
			VALUES($6,$7,'Settled sweep candidate token',$1,$8,$9,6,'TRC-20',100);
			INSERT INTO cat_wallets(
				id,code,name,network_id,merchant_id,address,tier,wallet_role,status,
				allocation_mode,is_active,chain_activation_status)
			VALUES($10,$11,'Settled sweep candidate wallet',$1,$4,$12,'pool','merchant_pool','free',
			       'transient',TRUE,'active');
			INSERT INTO doc_crypto_invoices(
				id,public_ref,number,date,merchant_id,token_id,wallet_id,expected_amount,
				status,expires_at,ttl_minutes,creation_source)
			VALUES($13,$14,$15,$16::timestamptz,$4,$6,$10,200,'completed',$16::timestamptz+interval '1 hour',60,'portal');
			INSERT INTO doc_crypto_payments(
				id,number,date,basis_type,basis_id,posted,posted_version,invoice_id,merchant_id,
				network_id,token_id,wallet_id,tx_hash,from_address,amount,event_kind,event_index,
				block_number,block_hash,confirmations,required_confs,finality_status,canonicality,
				status,detected_at,confirmed_at)
			VALUES($17,$18,$16,'CryptoInvoice',$13,TRUE,1,$13,$4,$1,$6,$10,$19,$20,200,
			       'contract_log',0,100,$21,20,20,'finalized','canonical','settled',$16,$16)
		`, pgx.QueryExecModeSimpleProtocol,
			networkID, "SSC-NET-"+suffix, "settled-sweep-"+suffix,
			merchantID, "SSC-MERCHANT-"+suffix,
			tokenID, "SSC-TOKEN-"+suffix, "TSettledSweepToken"+suffix, "SS"+suffix[:6],
			walletID, "SSC-WALLET-"+suffix, "TSettledSweepWallet"+suffix,
			invoiceID, "ci_"+suffix, "SSC-INVOICE-"+suffix, now,
			paymentID, "SSC-PAYMENT-"+suffix, "settled-sweep-tx-"+suffix,
			"TSettledSweepSender"+suffix, "settled-sweep-block-"+suffix)
		require.NoError(t, insertErr)

		candidates, queryErr := (&CryptoProcessor{}).querySweepCandidates(txCtx)
		require.NoError(t, queryErr)
		var matched *sweepCandidate
		for i := range candidates {
			if candidates[i].walletID == walletID && candidates[i].tokenID == tokenID {
				matched = &candidates[i]
				break
			}
		}
		require.NotNil(t, matched)
		require.Equal(t, "200", matched.balance.String())
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}

func TestClaimSweepPendingWalletsRetainsConfiguredRawTokenUnit(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	errRollback := errors.New("rollback integration fixture")
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		networkID, tokenID, merchantID := id.New(), id.New(), id.New()
		hotWalletID := id.New()
		zeroWalletID, reserveWalletID := id.New(), id.New()
		persistentWalletID, transientWalletID := id.New(), id.New()
		suffix := id.Compact(networkID)

		_, err := q.Exec(txCtx, `
			INSERT INTO cat_blockchain_networks (
				id, code, name, chain_family, network_identifier,
				native_token_symbol, native_decimals, confirmations_needed
			) VALUES ($1, $2, 'Sweep reserve test', 'tron', $3, 'TRX', 6, 1)
		`, networkID, "RESERVE-"+suffix, "reserve-"+suffix)
		require.NoError(t, err)

		_, err = q.Exec(txCtx, `
			INSERT INTO cat_tokens (
				id, code, name, network_id, contract_address, symbol,
				decimal_places, token_standard, sweep_retain_amount
			) VALUES ($1, $2, 'Test USDT', $3, $4, 'USDT', 6, 'TRC-20', 1)
		`, tokenID, "RESERVE-USDT-"+suffix, networkID, "TContract"+suffix)
		require.NoError(t, err)

		_, err = q.Exec(txCtx, `
			INSERT INTO cat_merchants (id, code, name)
			VALUES ($1, $2, 'Sweep reserve merchant')
		`, merchantID, "RESERVE-MERCHANT-"+suffix)
		require.NoError(t, err)

		_, err = q.Exec(txCtx, `
			INSERT INTO cat_wallets (
				id, code, name, network_id, merchant_id, address,
				tier, wallet_role, status, allocation_mode, customer_id,
				vault_key_id, chain_activation_status
			) VALUES
				($1, $6, 'Hot', $5, NULL, $11, 'hot', 'liquidity_hot', 'free', 'transient', '', $16, 'active'),
				($2, $7, 'Zero', $5, $10, $12, 'pool', 'merchant_pool', 'sweep_pending', 'persistent', 'zero', $17, 'active'),
				($3, $8, 'Reserve only', $5, $10, $13, 'pool', 'merchant_pool', 'sweep_pending', 'persistent', 'reserve', $18, 'active'),
				($4, $9, 'Persistent ordinary', $5, $10, $14, 'pool', 'merchant_pool', 'sweep_pending', 'persistent', 'ordinary', $19, 'active'),
				($20, $21, 'Transient ordinary', $5, $10, $15, 'pool', 'merchant_pool', 'sweep_pending', 'transient', '', $22, 'active')
		`, hotWalletID, zeroWalletID, reserveWalletID, persistentWalletID,
			networkID,
			"RSV-HOT-"+suffix, "RSV-ZERO-"+suffix,
			"RSV-MIN-"+suffix, "RSV-PERSIST-"+suffix,
			merchantID,
			"THot"+suffix, "TZero"+suffix, "TReserve"+suffix,
			"TPersistent"+suffix, "TTransient"+suffix,
			"vault/hot/"+suffix, "vault/zero/"+suffix,
			"vault/reserve/"+suffix, "vault/persistent/"+suffix,
			transientWalletID, "RSV-TRANSIENT-"+suffix, "vault/transient/"+suffix,
		)
		require.NoError(t, err)

		_, err = q.Exec(txCtx, `
			INSERT INTO reg_crypto_balance_balances (wallet_id, token_id, amount)
			VALUES ($1, $5, 0), ($2, $5, 1), ($3, $5, 100000000), ($4, $5, 100000000)
		`, zeroWalletID, reserveWalletID, persistentWalletID, transientWalletID, tokenID)
		require.NoError(t, err)

		repo := NewOutboundRepo()
		tasks, err := repo.ClaimSweepPendingWallets(txCtx, 10)
		require.NoError(t, err)
		require.Len(t, tasks, 2)

		amountByWallet := make(map[id.ID]string, len(tasks))
		for _, task := range tasks {
			amountByWallet[task.WalletID] = task.Balance.String()
		}
		require.Equal(t, "99999999", amountByWallet[persistentWalletID])
		require.Equal(t, "99999999", amountByWallet[transientWalletID])
		require.NotContains(t, amountByWallet, zeroWalletID)
		require.NotContains(t, amountByWallet, reserveWalletID)

		for _, task := range tasks {
			require.NoError(t, repo.MarkSweepAttemptBroadcast(txCtx, task.AttemptID))
		}
		require.NoError(t, repo.MarkSweepAttemptsPosted(txCtx, outboundapp.SweepGroupAttemptIDs(tasks)))
		require.NoError(t, repo.MarkWalletsFreeAfterSweep(txCtx, []id.ID{persistentWalletID, transientWalletID}))
		var persistentStatus, transientStatus wallet.WalletStatus
		require.NoError(t, q.QueryRow(txCtx, `SELECT status FROM cat_wallets WHERE id=$1`, persistentWalletID).Scan(&persistentStatus))
		require.NoError(t, q.QueryRow(txCtx, `SELECT status FROM cat_wallets WHERE id=$1`, transientWalletID).Scan(&transientStatus))
		require.Equal(t, wallet.WalletStatusAssigned, persistentStatus)
		require.Equal(t, wallet.WalletStatusFree, transientStatus)

		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}

func TestConcurrentWorkersClaimOneSweepPendingWalletOnce(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	networkID, tokenID, hotWalletID, poolWalletID := id.New(), id.New(), id.New(), id.New()
	suffix := id.Compact(networkID)
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_blockchain_networks(
			id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,confirmations_needed)
		VALUES($1,$2,'Sweep claim race network','tron',$3,'TRX',6,1);
		INSERT INTO cat_tokens(
			id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
		VALUES(gen_random_uuid_v7(),$2||'-N','Sweep claim native token',$1,'','TRX',6,'native');
		INSERT INTO cat_tokens(
			id,code,name,network_id,contract_address,symbol,decimal_places,token_standard,sweep_retain_amount)
		VALUES($4,$5,'Sweep claim race token',$1,$6,$7,6,'TRC-20',0);
		INSERT INTO cat_wallets(
			id,code,name,network_id,address,tier,wallet_role,status,allocation_mode,vault_key_id,chain_activation_status)
		VALUES
			($8,$9,'Sweep claim hot',$1,$10,'hot','liquidity_hot','free','transient',$11,'active'),
			($12,$13,'Sweep claim pool',$1,$14,'pool','merchant_pool','sweep_pending','transient',$15,'active');
		INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount)
		VALUES($12,$4,42000000)
	`, pgx.QueryExecModeSimpleProtocol, networkID, "SC-NET-"+suffix, "sweep-claim-race-"+suffix,
		tokenID, "SC-TOKEN-"+suffix, "TSweepClaimToken"+suffix, "SC"+suffix[:6],
		hotWalletID, "SC-HOT-"+suffix, "TSweepClaimHot"+suffix, "vault/sweep-claim-hot/"+suffix,
		poolWalletID, "SC-POOL-"+suffix, "TSweepClaimPool"+suffix, "vault/sweep-claim-pool/"+suffix)
	require.NoError(t, err)
	// This committed fixture remains visible to subsequent runtime discovery.
	// A token-only active network would make later catalog loads fail closed.
	networks, err := (&CryptoProcessor{}).loadNetworksFiltered(ctx, true, "SC-NET-"+suffix)
	require.NoError(t, err)
	require.Len(t, networks, 1)
	require.Len(t, networks[0].assets, 2)

	repo := NewOutboundRepo()
	results := make(chan []SweepTask, 2)
	errorsCh := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tasks, claimErr := repo.ClaimSweepPendingWallets(ctx, 1)
			results <- tasks
			errorsCh <- claimErr
		}()
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	for claimErr := range errorsCh {
		require.NoError(t, claimErr)
	}
	var claimedTasks int
	for tasks := range results {
		claimedTasks += len(tasks)
	}
	require.Equal(t, 1, claimedTasks)

	var attempts int
	var status wallet.WalletStatus
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM reg_crypto_sweep_attempts WHERE wallet_id=$1 AND token_id=$2),
			(SELECT status FROM cat_wallets WHERE id=$1)
	`, poolWalletID, tokenID).Scan(&attempts, &status))
	require.Equal(t, 1, attempts)
	require.Equal(t, wallet.WalletStatusSweeping, status)
}
