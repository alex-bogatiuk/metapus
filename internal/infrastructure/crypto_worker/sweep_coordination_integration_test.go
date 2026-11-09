//go:build integration

package crypto_worker

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/infrastructure/storage/postgres"
)

type sweepCoordinationFixture struct {
	network, native, token, hot, wallet id.ID
	hotAddress, walletAddress           string
}

func withSweepCoordinationFixture(t *testing.T, fn func(context.Context, *postgres.TxManager, sweepCoordinationFixture)) {
	runSweepCoordinationFixture(t, false, fn)
}

func runSweepCoordinationFixture(t *testing.T, committed bool, fn func(context.Context, *postgres.TxManager, sweepCoordinationFixture)) {
	t.Helper()
	runSweepCoordinationFixtureWithAddresses(t, committed, "", "", fn)
}

func runSweepCoordinationFixtureWithAddresses(t *testing.T, committed bool, hotAddress, depositAddress string, fn func(context.Context, *postgres.TxManager, sweepCoordinationFixture)) {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	require.NotEmpty(t, dsn, "a disposable integration database is required")
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx := tenant.WithTxManager(context.Background(), txm)
	rollback := errors.New("rollback fixture")
	f := sweepCoordinationFixture{network: id.New(), native: id.New(), token: id.New(), hot: id.New(), wallet: id.New()}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		suffix := id.Compact(f.network)
		if hotAddress == "" {
			hotAddress = testTRONAddress()
		}
		if depositAddress == "" {
			depositAddress = testTRONAddress()
		}
		f.hotAddress, f.walletAddress = hotAddress, depositAddress
		_, err := txm.GetQuerier(ctx).Exec(ctx, `
			INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
			VALUES($1,$6,'Sweep coordination','tron',$6,'TRX',6);
			INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address,sweep_retain_amount)
			VALUES($2,$6||'-N','Native',$1,'TRX',6,'native','',0),
			      ($3,$6||'-T','Token',$1,'USDT',6,'TRC-20',$6||'-contract',1);
			INSERT INTO cat_wallets(id,code,name,network_id,address,tier,wallet_role,status,vault_key_id,chain_activation_status)
			VALUES($4,$6||'-H','Hot',$1,$7,'hot','liquidity_hot','free','test/hot','active'),
			      ($5,$6||'-W','Deposit',$1,$8,'pool','merchant_pool','sweep_pending','test/deposit','active');
			INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount)
			VALUES($5,$2,200000000),($5,$3,1000000);
		`, pgx.QueryExecModeSimpleProtocol, f.network, f.native, f.token, f.hot, f.wallet, "COORD-"+suffix, hotAddress, depositAddress)
		require.NoError(t, err)
		if committed {
			return nil
		}
		fn(ctx, txm, f)
		return rollback
	})
	if !committed {
		require.ErrorIs(t, err, rollback)
		return
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE cat_wallets SET is_active=FALSE WHERE network_id=$1`, f.network)
	})
	fn(ctx, txm, f)
}

func TestSweepCoordinationDefersNativeWhileTokenNeedsGas(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		tasks, err := NewOutboundRepo().ClaimSweepPendingWallets(ctx, 100)
		require.NoError(t, err)
		var own []SweepTask
		for _, task := range tasks {
			if task.WalletID == f.wallet {
				own = append(own, task)
			}
		}
		require.Len(t, own, 1, "native gas must not be drained in the token sweep cycle")
		require.Equal(t, token.TokenStandardTRC20, own[0].TokenStandard)
		require.Equal(t, "999999", own[0].Balance.String())
	})
}

func TestSweepCoordinationKeepsWalletUntilEverySiblingFinishes(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		q := txm.GetQuerier(ctx)
		nativeAttempt, tokenAttempt := id.New(), id.New()
		_, err := q.Exec(ctx, `
			UPDATE cat_wallets SET status='sweeping' WHERE id=$1;
			INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at)
			VALUES($6,$1,$2,$4,$5,'source','destination',100,'broadcast',NOW()+interval '10 minutes'),
			      ($7,$1,$3,$4,$5,'source','destination',999999,'claimed',NOW()+interval '10 minutes');
		`, pgx.QueryExecModeSimpleProtocol, f.wallet, f.native, f.token, f.network, f.hot, nativeAttempt, tokenAttempt)
		require.NoError(t, err)
		repo := NewOutboundRepo()
		require.NoError(t, repo.MarkSweepAttemptsPosted(ctx, []id.ID{nativeAttempt}))
		require.NoError(t, repo.MarkWalletsFreeAfterSweep(ctx, []id.ID{f.wallet}))
		var status string
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM cat_wallets WHERE id=$1`, f.wallet).Scan(&status))
		require.Equal(t, "sweeping", status)
		require.NoError(t, repo.ReleaseSweepClaims(ctx, []id.ID{f.wallet}))
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM cat_wallets WHERE id=$1`, f.wallet).Scan(&status))
		require.Equal(t, "sweeping", status, "a failed sibling must not strand a runnable sibling")
		tasks, err := repo.ClaimRunnableSweepAttempts(ctx, 100)
		require.NoError(t, err)
		require.True(t, containsSweepAttempt(tasks, tokenAttempt))
		require.NoError(t, repo.MarkSweepAttemptBroadcast(ctx, tokenAttempt))
		require.NoError(t, repo.MarkSweepAttemptsPosted(ctx, []id.ID{tokenAttempt}))
		require.NoError(t, repo.MarkWalletsFreeAfterSweep(ctx, []id.ID{f.wallet}))
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM cat_wallets WHERE id=$1`, f.wallet).Scan(&status))
		require.Equal(t, "free", status)
	})
}

func TestSweepCoordinationDoesNotExpireWhileFundingIsDurable(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		q := txm.GetQuerier(ctx)
		attemptID, fundingID := id.New(), id.New()
		_, err := q.Exec(ctx, `
			UPDATE cat_wallets SET status='sweeping' WHERE id=$1;
			INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at,updated_at)
			VALUES($5,$1,$2,$3,$4,'source','destination',999999,'signing',NOW()-interval '1 hour',NOW()-interval '2 minutes');
			INSERT INTO doc_crypto_native_fundings(id,basis_type,basis_id,attempt,network_id,native_token_id,source_wallet_id,destination_wallet_id,amount_minor,status,tx_hash)
			VALUES($6,'CryptoSweepAttempt',$5,1,$3,$7,$4,$1,100,'broadcast',$6::text);
		`, pgx.QueryExecModeSimpleProtocol, f.wallet, f.token, f.network, f.hot, attemptID, fundingID, f.native)
		require.NoError(t, err)
		repo := NewOutboundRepo()
		require.NoError(t, repo.ExpireStaleSweepAttempts(ctx))
		var status string
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM reg_crypto_sweep_attempts WHERE id=$1`, attemptID).Scan(&status))
		require.Equal(t, "signing", status, "the signing TTL must not discard an externally funded parent")
		tasks, err := repo.ClaimRunnableSweepAttempts(ctx, 100)
		require.NoError(t, err)
		require.True(t, containsSweepAttempt(tasks, attemptID))
	})
}

func TestSweepCoordinationExpiryDoesNotStrandSibling(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		q := txm.GetQuerier(ctx)
		expired, active := id.New(), id.New()
		_, err := q.Exec(ctx, `
			UPDATE cat_wallets SET status='sweeping' WHERE id=$1;
			INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at)
			VALUES($6,$1,$2,$4,$5,'source','destination',100,'claimed',NOW()-interval '1 minute'),
			      ($7,$1,$3,$4,$5,'source','destination',999999,'claimed',NOW()+interval '10 minutes');
		`, pgx.QueryExecModeSimpleProtocol, f.wallet, f.native, f.token, f.network, f.hot, expired, active)
		require.NoError(t, err)
		require.NoError(t, NewOutboundRepo().ExpireStaleSweepAttempts(ctx))
		var status string
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM cat_wallets WHERE id=$1`, f.wallet).Scan(&status))
		require.Equal(t, "sweeping", status)
		tasks, err := NewOutboundRepo().ClaimRunnableSweepAttempts(ctx, 100)
		require.NoError(t, err)
		require.True(t, containsSweepAttempt(tasks, active))
		require.False(t, containsSweepAttempt(tasks, expired))
	})
}

func TestSweepCoordinationConcurrentSiblingCompletionReleasesOnce(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		first, second := id.New(), id.New()
		_, err := txm.GetQuerier(ctx).Exec(ctx, `
			UPDATE cat_wallets SET status='sweeping' WHERE id=$1;
			INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at)
			VALUES($6,$1,$2,$4,$5,'source','destination',100,'broadcast',NOW()+interval '10 minutes'),
			      ($7,$1,$3,$4,$5,'source','destination',999999,'broadcast',NOW()+interval '10 minutes');
		`, pgx.QueryExecModeSimpleProtocol, f.wallet, f.native, f.token, f.network, f.hot, first, second)
		require.NoError(t, err)
		service := cryptoapp.NewSweepLifecycleService(contextTxManager{}, NewOutboundRepo())
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, attempt := range []id.ID{first, second} {
			wg.Go(func() { <-start; errs <- service.Complete(ctx, []id.ID{f.wallet}, []id.ID{attempt}) })
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		var status string
		require.NoError(t, txm.GetQuerier(ctx).QueryRow(ctx, `SELECT status FROM cat_wallets WHERE id=$1`, f.wallet).Scan(&status))
		require.Equal(t, "free", status)
	})
}

func TestSweepCoordinationNativeOnlyRemainsRunnable(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		_, err := txm.GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_balance_balances SET amount=1 WHERE wallet_id=$1 AND token_id=$2`, f.wallet, f.token)
		require.NoError(t, err)
		tasks, err := NewOutboundRepo().ClaimSweepPendingWallets(ctx, 100)
		require.NoError(t, err)
		var own []SweepTask
		for _, task := range tasks {
			if task.WalletID == f.wallet {
				own = append(own, task)
			}
		}
		require.Len(t, own, 1)
		require.Equal(t, f.native, own[0].TokenID)
	})
}

func TestSweepCoordinationRecoveryIgnoresRetainedDust(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		q := txm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `
			UPDATE cat_wallets SET status='free' WHERE id=$1;
			UPDATE reg_crypto_balance_balances SET amount=1 WHERE wallet_id=$1 AND token_id=$2;
			INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at)
			VALUES($5,$1,$2,$3,$4,'source','destination',999999,'expired',NOW()-interval '1 hour');
		`, pgx.QueryExecModeSimpleProtocol, f.wallet, f.token, f.network, f.hot, id.New())
		require.NoError(t, err)
		require.NoError(t, NewOutboundRepo().ExpireStaleSweepAttempts(ctx))
		var status string
		require.NoError(t, q.QueryRow(ctx, `SELECT status FROM cat_wallets WHERE id=$1`, f.wallet).Scan(&status))
		require.Equal(t, "free", status, "historical failures must not trap a reserve-only wallet in sweep_pending")
	})
}

func TestSweepCoordinationSignedAttemptRemainsRecoverableAfterTTL(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
		q := txm.GetQuerier(ctx)
		attemptID, authID := id.New(), id.New()
		_, err := q.Exec(ctx, `
			UPDATE cat_wallets SET status='sweeping' WHERE id=$1;
			INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at,updated_at)
			VALUES($5,$1,$2,$3,$4,'source','destination',999999,'signing',NOW()-interval '1 hour',NOW()-interval '2 minutes');
			INSERT INTO reg_crypto_signing_authorizations(
				id,authorization_id,tenant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,
				source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,contract_address,amount_minor_units,
				max_network_fee_minor,basis_type,basis_id,attempt,generation,expires_at,policy_version,status,authorization_jws,
				tx_hash,unsigned_fingerprint,unsigned_envelope,signed_envelope)
			VALUES($6,$6::text,$7,'sweep',1,$3,'test','tron','nile','source','source','test/key','destination','TRC-20','contract',
			       999999,100,'CryptoSweepAttempt',$5,1,1,NOW()-interval '1 hour','tron-v1','signed','test-jws','tx','fp','{}','{}');
		`, pgx.QueryExecModeSimpleProtocol, f.wallet, f.token, f.network, f.hot, attemptID, authID, id.New().String())
		require.NoError(t, err)
		repo := NewOutboundRepo()
		require.NoError(t, repo.ExpireStaleSweepAttempts(ctx))
		tasks, err := repo.ClaimRunnableSweepAttempts(ctx, 100)
		require.NoError(t, err)
		require.True(t, containsSweepAttempt(tasks, attemptID), "reuse the old envelope; do not expire or create a second attempt")
	})
}

func TestSweepCoordinationRetriesExactSavedUnsignedAuthorizationOnlyBeforeTTL(t *testing.T) {
	for _, expired := range []bool{false, true} {
		withSweepCoordinationFixture(t, func(ctx context.Context, txm *postgres.TxManager, f sweepCoordinationFixture) {
			attempt, auth, fee := id.New(), id.New(), id.New()
			ttl := "10 minutes"
			if expired {
				ttl = "-10 minutes"
			}
			q := txm.GetQuerier(ctx)
			_, err := q.Exec(ctx, `UPDATE cat_wallets SET status='sweeping' WHERE id=$1;
    INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at,updated_at)
    VALUES($5,$1,$2,$3,$4,'source','destination',999999,'held',NOW()+interval '10 minutes',NOW()-interval '2 minutes');
    INSERT INTO reg_crypto_signing_authorizations(id,authorization_id,tenant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,contract_address,amount_minor_units,max_network_fee_minor,basis_type,basis_id,attempt,generation,expires_at,policy_version,status,authorization_jws,unsigned_fingerprint,unsigned_envelope)
    VALUES($6,$6::text,$7,'sweep',1,$3,'test','tron','nile','source','source','test/key','destination','TRC-20','contract',999999,100,'CryptoSweepAttempt',$5,1,1,NOW()+$8::interval,'tron-v1','created','saved-jws','saved-fingerprint','{"saved":true}');
    INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,status)
    VALUES($9,$3,$1,$10,$2,'CryptoSweepAttempt',$5,'sweep',1,'native_burn',100,'reserved');`, pgx.QueryExecModeSimpleProtocol, f.wallet, f.token, f.network, f.hot, attempt, auth, id.New().String(), ttl, fee, f.native)
			require.NoError(t, err)
			tasks, err := NewOutboundRepo().ClaimRunnableSweepAttempts(ctx, 100)
			require.NoError(t, err)
			require.Equal(t, !expired, containsSweepAttempt(tasks, attempt))
			var count int
			var generation int
			var fingerprint string
			require.NoError(t, q.QueryRow(ctx, `SELECT COUNT(*),MAX(generation),MAX(unsigned_fingerprint) FROM reg_crypto_signing_authorizations WHERE basis_id=$1`, attempt).Scan(&count, &generation, &fingerprint))
			require.Equal(t, 1, count)
			require.Equal(t, 1, generation)
			require.Equal(t, "saved-fingerprint", fingerprint)
		})
	}
}

// testTRONAddress returns a unique syntactically valid TRON base58 address.
func testTRONAddress() string {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	raw := id.New()
	out := []byte{'T'}
	for i := 0; len(out) < 34; i++ {
		out = append(out, alphabet[int(raw[i%len(raw)]+byte(i*7))%len(alphabet)])
	}
	return string(out)
}
