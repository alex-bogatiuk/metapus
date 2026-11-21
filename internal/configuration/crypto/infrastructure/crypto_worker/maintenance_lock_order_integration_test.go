//go:build integration

package crypto_worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestNativeFundingOperationalUpdatesLockWalletBeforeFunding(t *testing.T) {
	for _, operation := range []string{"status", "broadcast", "release", "replacement"} {
		t.Run(operation, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
				funding, _, receipt := seedNativeFunding(t, ctx, tm, f, 1, false)
				_, err := tm.GetQuerier(ctx).Exec(ctx, "UPDATE doc_crypto_native_fundings SET claimed_by='test-owner' WHERE id=$1", funding)
				require.NoError(t, err)
				assertWorkerResourceOrder(t, ctx,
					func(ctx context.Context) error {
						return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, f.wallet)
					},
					func(ctx context.Context) error {
						_, err := tm.GetQuerier(ctx).Exec(ctx, "SELECT id FROM doc_crypto_native_fundings WHERE id=$1 FOR UPDATE", funding)
						return err
					},
					func(ctx context.Context) error {
						store := nativeFundingStore{OutboundRepo: NewOutboundRepo()}
						switch operation {
						case "status":
							return store.SetStatus(ctx, funding, "held", "retryable", "", nil)
						case "broadcast":
							return store.MarkBroadcast(ctx, funding, receipt.TxHash, "saved-fingerprint")
						case "replacement":
							return NewOutboundRepo().UpdateOutboundReplacementHash(ctx, "CryptoNativeFunding", funding, receipt.TxHash, receipt.TxHash)
						default:
							return (nativeFundingConfirmationStore{}).ReleaseNativeFundingConfirmation(ctx, funding, "test-owner")
						}
					})
			})
		})
	}
}

func TestNativeFundingConfirmationSkipsBusyFootprintWithoutLeakingLocks(t *testing.T) {
	for _, resource := range []string{"wallet", "funding"} {
		t.Run(resource, func(t *testing.T) {
			runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
				funding, _, _ := seedNativeFunding(t, ctx, tm, f, 1, false)
				runSweepCoordinationFixture(t, true, func(_ context.Context, _ *postgres.TxManager, other sweepCoordinationFixture) {
					available, _, _ := seedNativeFunding(t, ctx, tm, other, 1, false)
					holder := make(chan struct{})
					locked, done := make(chan error, 1), make(chan error, 1)
					go func() {
						done <- tm.RunInTransaction(ctx, func(ctx context.Context) error {
							query := "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE"
							key := f.wallet
							if resource == "funding" {
								query, key = "SELECT id FROM doc_crypto_native_fundings WHERE id=$1 FOR UPDATE", funding
							}
							_, err := tm.GetQuerier(ctx).Exec(ctx, query, key)
							locked <- err
							if err != nil {
								return err
							}
							select {
							case <-holder:
								return nil
							case <-ctx.Done():
								return ctx.Err()
							}
						})
					}()
					defer func() { close(holder); require.NoError(t, <-done) }()
					require.NoError(t, <-locked)
					bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
					defer cancel()
					require.NoError(t, tm.RunInTransaction(bounded, func(ctx context.Context) error {
						batch, err := (nativeFundingConfirmationStore{}).ClaimNativeFundingConfirmations(ctx, "selector", 1000)
						if err != nil {
							return err
						}
						found := false
						for _, item := range batch {
							require.NotEqual(t, funding, item.ID)
							found = found || item.ID == available
						}
						require.True(t, found)
						// The skipped funding's other wallet must be free even
						// while the selector's successful transaction is open.
						_, err = tm.GetQuerier(bounded).Exec(bounded, "SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE NOWAIT", f.hot)
						return err
					}))
				})
			})
		})
	}
}

func seedUnsignedMaintenanceAttempt(t *testing.T, ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) (id.ID, id.ID, id.ID) {
	t.Helper()
	attempt, auth, fee := id.New(), id.New(), id.New()
	_, err := tm.GetQuerier(ctx).Exec(ctx, `
        UPDATE cat_wallets SET status='sweeping' WHERE id=$1;
        INSERT INTO reg_crypto_sweep_attempts(id,wallet_id,token_id,network_id,hot_wallet_id,source_address,destination_address,amount,status,expires_at)
        VALUES($5,$1,$2,$3,$4,'source','destination',999999,'held',NOW()-interval '1 hour');
        INSERT INTO reg_crypto_signing_authorizations(id,authorization_id,tenant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,contract_address,amount_minor_units,max_network_fee_minor,basis_type,basis_id,attempt,generation,expires_at,policy_version,status,authorization_jws,unsigned_fingerprint,unsigned_envelope)
        VALUES($6,$6::text,$7,'sweep',1,$3,'test','tron','nile','source','source','test/key','destination','TRC-20','contract',999999,100,'CryptoSweepAttempt',$5,1,1,NOW()-interval '1 hour','tron-v1','created','saved-jws','saved-fingerprint-'||$5::text,'{"saved":true}');
        INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,status)
        VALUES($8,$3,$1,$9,$2,'CryptoSweepAttempt',$5,'sweep',1,'native_burn',100,'reserved');`,
		pgx.QueryExecModeSimpleProtocol, f.wallet, f.token, f.network, f.hot, attempt, auth, id.New().String(), fee, f.native)
	require.NoError(t, err)
	return attempt, auth, fee
}

func TestSweepMaintenanceLocksWalletBeforeAuthorization(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
		attempt, auth, fee := seedUnsignedMaintenanceAttempt(t, ctx, tm, f)
		assertWorkerResourceOrder(t, ctx,
			func(ctx context.Context) error {
				return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, f.wallet)
			},
			func(ctx context.Context) error {
				_, err := tm.GetQuerier(ctx).Exec(ctx, "SELECT id FROM reg_crypto_signing_authorizations WHERE id=$1 FOR UPDATE", auth)
				return err
			}, NewOutboundRepo().ExpireStaleSweepAttempts)
		var attemptStatus, authStatus, feeStatus, walletStatus string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT a.status,s.status,f.status,w.status FROM reg_crypto_sweep_attempts a
            JOIN reg_crypto_signing_authorizations s ON s.id=$2 JOIN reg_crypto_fee_reservations f ON f.id=$3
            JOIN cat_wallets w ON w.id=a.wallet_id WHERE a.id=$1`, attempt, auth, fee).Scan(&attemptStatus, &authStatus, &feeStatus, &walletStatus))
		require.Equal(t, []string{"expired", "failed", "released", "sweep_pending"}, []string{attemptStatus, authStatus, feeStatus, walletStatus})
	})
}

func TestSweepMaintenanceDoesNotExpandFrozenCohort(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
		first, firstAuth, firstFee := seedUnsignedMaintenanceAttempt(t, ctx, tm, f)
		repo := NewOutboundRepo()
		batch, err := repo.PrepareSweepMaintenance(ctx)
		require.NoError(t, err)
		other := f
		other.wallet = id.New()
		_, err = tm.GetQuerier(ctx).Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,tier,wallet_role,status)
			VALUES($1,$1::uuid::text,'Late maintenance wallet',$2,$1::uuid::text,'late-key','pool','merchant_pool','sweeping')`, other.wallet, f.network)
		require.NoError(t, err)
		late, lateAuth, lateFee := seedUnsignedMaintenanceAttempt(t, ctx, tm, other)
		require.NoError(t, repo.ExpireUnsignedSweepAttempts(ctx, batch))
		for i, keys := range [][]id.ID{{first, firstAuth, firstFee}, {late, lateAuth, lateFee}} {
			var attempt, auth, fee string
			require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT a.status,s.status,f.status FROM reg_crypto_sweep_attempts a
                JOIN reg_crypto_signing_authorizations s ON s.id=$2 JOIN reg_crypto_fee_reservations f ON f.id=$3 WHERE a.id=$1`, keys[0], keys[1], keys[2]).Scan(&attempt, &auth, &fee))
			expected := []string{"expired", "failed", "released"}
			if i == 1 {
				expected = []string{"held", "created", "reserved"}
			}
			require.Equal(t, expected, []string{attempt, auth, fee})
		}
	})
}

func TestNativeFundingConcurrentCreationValidatesImmutableReplay(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
		candidate := crypto.NativeFunding{BasisType: "CryptoSweepAttempt", BasisID: id.New(), Attempt: 1,
			NetworkID: f.network, NativeTokenID: f.native, SourceWalletID: f.hot, DestinationWalletID: f.wallet, Amount: types.MustRawAmount("100")}
		store := nativeFundingStore{OutboundRepo: NewOutboundRepo()}
		type result struct {
			value crypto.NativeFunding
			err   error
		}
		results := make(chan result, 8)
		var workers sync.WaitGroup
		for range 8 {
			workers.Go(func() {
				intent := candidate
				intent.ID = id.New()
				value, err := store.Create(ctx, intent)
				results <- result{value, err}
			})
		}
		workers.Wait()
		close(results)
		var actual id.ID
		for item := range results {
			require.NoError(t, item.err)
			if id.IsNil(actual) {
				actual = item.value.ID
			}
			require.Equal(t, actual, item.value.ID)
		}
		candidate.ID = id.New()
		candidate.Amount = types.MustRawAmount("101")
		_, err := store.Create(ctx, candidate)
		require.ErrorContains(t, err, "immutable intent")
		var count, movements int
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT (SELECT count(*) FROM doc_crypto_native_fundings WHERE basis_type=$1 AND basis_id=$2),
            (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$3)`, candidate.BasisType, candidate.BasisID, actual).Scan(&count, &movements))
		require.Equal(t, 1, count)
		require.Zero(t, movements)
		ownFee, foreignFee := id.New(), id.New()
		_, err = tm.GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,reserved_principal_minor,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,status)
            VALUES($1,$3,$4,$5,$5,100,'CryptoNativeFunding',$6,'native_funding',1,'native_funding',10,'reserved'),
                  ($2,$3,$4,$5,$5,100,'CryptoNativeFunding',$7,'native_funding',1,'native_funding',10,'reserved')`, ownFee, foreignFee, f.network, f.hot, f.native, actual, id.New())
		require.NoError(t, err)
		require.ErrorContains(t, store.LinkReservation(ctx, actual, foreignFee), "another intent")
		require.NoError(t, store.LinkReservation(ctx, actual, ownFee))
	})
}

func TestSweepMaintenanceRechecksSignedEnvelopeAfterPreparation(t *testing.T) {
	withSweepCoordinationFixture(t, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
		attempt, auth, fee := seedUnsignedMaintenanceAttempt(t, ctx, tm, f)
		repo := NewOutboundRepo()
		batch, err := repo.PrepareSweepMaintenance(ctx)
		require.NoError(t, err)
		_, err = tm.GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_signing_authorizations SET status='signed',signed_envelope='{}',tx_hash='already-signed' WHERE id=$1`, auth)
		require.NoError(t, err)
		require.NoError(t, repo.ExpireUnsignedSweepAttempts(ctx, batch))
		var attemptStatus, authStatus, feeStatus string
		require.NoError(t, tm.GetQuerier(ctx).QueryRow(ctx, `SELECT a.status,s.status,f.status FROM reg_crypto_sweep_attempts a
            JOIN reg_crypto_signing_authorizations s ON s.id=$2 JOIN reg_crypto_fee_reservations f ON f.id=$3 WHERE a.id=$1`, attempt, auth, fee).Scan(&attemptStatus, &authStatus, &feeStatus))
		require.Equal(t, []string{"held", "signed", "reserved"}, []string{attemptStatus, authStatus, feeStatus})
	})
}
