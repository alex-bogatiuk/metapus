//go:build integration

package crypto_repo

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	dbassets "metapus/db"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/migration"
)

func amlTestDatabase(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_ADMIN_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_ADMIN_DSN is required for isolated AML integration tests")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	name := "aml_test_" + id.Compact(id.New())
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	address, err := url.Parse(dsn)
	require.NoError(t, err)
	address.Path = "/" + name
	_, err = migration.RunAll(address.String())
	require.NoError(t, err)
	pool, err := postgres.OpenPool(t.Context(), address.String())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	ctx := tenant.WithTxManager(t.Context(), postgres.NewTxManagerFromRawPool(pool))
	return ctx, pool
}

func seedAMLJob(t *testing.T, ctx context.Context, pool *pgxpool.Pool, budget ...int64) (EnqueueAMLInput, id.ID) {
	t.Helper()
	merchantID, networkID, tokenID, walletID, accountID := id.New(), id.New(), id.New(), id.New(), id.New()
	suffix := id.Compact(merchantID)
	address := testTRONAddress()
	_, err := pool.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'AML test')`, merchantID, "AML-"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
		VALUES($1,$2,'AML test network','tron',$2,'TRX',6)`, networkID, "AML-"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
		VALUES($1,$2,'AML test asset',$3,'test-contract','USDT',6,'trc20')`, tokenID, "AML-"+suffix, networkID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,merchant_id,custody_merchant_id,address,vault_key_id,derivation_path)
		VALUES($1,$2,'AML owner wallet',$3,$4,$4,$5,'test-key','test-derived-path')`, walletID, "AML-"+suffix, networkID, merchantID, address)
	require.NoError(t, err)
	// The fixture has no production credentials; runtime tests install an encrypted synthetic key.
	_, err = pool.Exec(ctx, `INSERT INTO cat_integration_accounts(id,code,name,provider)
		VALUES($1,$2,'Synthetic AML account fixture','getblock_net')`, accountID, "AML-"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_aml_provider_mappings(network_id,token_id,version,contract_version,contract_address,currency,provider_token_id,native,decimal_places,capability_accepted,acceptance_reference,currencylist_snapshot,pending_statuses)
        VALUES($1,$2,'test-mapping-v1','synthetic-v1','test-contract','TRX','synthetic-provider-token',FALSE,6,TRUE,'synthetic fixture only','{"fixture":true}',ARRAY['PENDING'])`, networkID, tokenID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_aml_account_policies(account_id,allow_submissions) VALUES($1,TRUE)`, accountID)
	require.NoError(t, err)
	units := int64(100)
	if len(budget) > 0 {
		units = budget[0]
	}
	if units > 0 {
		_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_aml_package_credits(account_id,units,external_reference,authorized_by) VALUES($1,$2,'synthetic budget','integration test')`, accountID, units)
		require.NoError(t, err)
	}
	input := EnqueueAMLInput{WalletID: walletID, Provider: "getblock_net", ProviderAccountID: accountID, MappingVersion: "test-mapping-v1",
		DeadlineAt: time.Now().UTC().Add(time.Hour), Request: aml.CheckRequest{MerchantID: merchantID, NetworkID: networkID, TokenID: tokenID,
			Kind: aml.TransferSubject, ContractAddress: "test-contract", Address: address, TxHash: "tx-" + suffix,
			EventKind: "contract_log", BlockHash: "block-" + suffix, EconomicsHash: "economics-" + suffix, MatchingEffects: 1}}
	jobID, err := NewAMLRepository().EnqueueAMLCheck(ctx, input)
	require.NoError(t, err)
	return input, jobID
}

func TestAMLSubmitLeaseCrashNeverResubmitsPaidCheck(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	input, jobID := seedAMLJob(t, ctx, pool)
	repo := NewAMLRepository()
	duplicateID, err := repo.EnqueueAMLCheck(ctx, input)
	require.NoError(t, err)
	require.Equal(t, jobID, duplicateID)
	now := time.Now().UTC()
	job, ok, err := repo.ClaimAMLJob(ctx, now, time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, aml.JobSubmitting, job.Status)
	_, ok, err = repo.ClaimAMLJob(ctx, now.Add(2*time.Second), time.Second)
	require.NoError(t, err)
	require.False(t, ok)
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM reg_crypto_aml_jobs WHERE id=$1`, jobID).Scan(&status))
	require.Equal(t, string(aml.JobSubmissionUnknown), status)
	_, err = pool.Exec(ctx, `UPDATE reg_crypto_aml_jobs SET status='pending_submit' WHERE id=$1`, jobID)
	require.ErrorContains(t, err, "paid AML submit cannot be reset")
	_, err = pool.Exec(ctx, `UPDATE reg_crypto_aml_jobs SET request_digest=$2 WHERE id=$1`, jobID, fmt.Sprintf("%064d", 0))
	require.ErrorContains(t, err, "identity is immutable")
}

func TestAMLConcurrentWorkersClaimOnlyOneSubmit(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	_, _ = seedAMLJob(t, ctx, pool)
	var workers sync.WaitGroup
	claimed := make(chan bool, 8)
	errorsFound := make(chan error, 8)
	for range 8 {
		workers.Go(func() {
			_, ok, err := NewAMLRepository().ClaimAMLJob(ctx, time.Now().UTC(), time.Minute)
			claimed <- ok
			errorsFound <- err
		})
	}
	workers.Wait()
	close(claimed)
	close(errorsFound)
	count := 0
	for ok := range claimed {
		if ok {
			count++
		}
	}
	for err := range errorsFound {
		require.NoError(t, err)
	}
	require.Equal(t, 1, count)
}

func TestAMLPollRestartAndImmutableReport(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	_, _ = seedAMLJob(t, ctx, pool)
	repo := NewAMLRepository()
	now := time.Now().UTC()
	job, ok, err := repo.ClaimAMLJob(ctx, now, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	ticket := aml.Ticket{Provider: job.Provider, ProviderAccountID: job.ProviderAccountID, CheckHash: "test-check-hash",
		SubmitRequestID: job.SubmitRequestID, RequestDigest: job.RequestDigest, MappingVersion: job.MappingVersion, Request: job.Request, SubmittedAt: now}
	require.NoError(t, repo.SaveAMLTicket(ctx, job, ticket, now))
	poll, ok, err := repo.ClaimAMLJob(ctx, now.Add(time.Second), time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, aml.JobPolling, poll.Status)
	require.Equal(t, ticket.CheckHash, poll.Ticket.CheckHash)
	recovered, ok, err := repo.ClaimAMLJob(ctx, now.Add(3*time.Second), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, ticket.ProviderAccountID, recovered.Ticket.ProviderAccountID)
	require.Equal(t, ticket.CheckHash, recovered.Ticket.CheckHash)
	require.NotEqual(t, poll.PollRequestID, recovered.PollRequestID)
	require.Error(t, repo.RescheduleAMLPoll(ctx, poll, now))
	score := aml.ScoreBP(560)
	report := aml.Report{ID: id.New(), Provider: job.Provider, ProviderAccountID: job.ProviderAccountID, CheckHash: ticket.CheckHash,
		RequestDigest: job.RequestDigest, MappingVersion: job.MappingVersion, ContractVersion: "test-contract-v1", AdapterVersion: "test-adapter-v1",
		Score: &score, Signals: []aml.Signal{{Name: "wallet", Value: "1"}}, CoverageAccepted: true,
		CompletedAt: now, ReceivedAt: now, RawResponse: []byte(`{"synthetic":"fixture"}`)}
	require.NoError(t, repo.SaveAMLReport(ctx, recovered, report))
	var storedScore int
	var hash string
	require.NoError(t, pool.QueryRow(ctx, `SELECT score_bp,response_hash FROM reg_crypto_aml_reports WHERE id=$1`, report.ID).Scan(&storedScore, &hash))
	require.Equal(t, 560, storedScore)
	require.Len(t, hash, 64)
	_, err = pool.Exec(ctx, `UPDATE reg_crypto_aml_reports SET score_bp=0 WHERE id=$1`, report.ID)
	require.ErrorContains(t, err, "append-only")
	_, err = pool.Exec(ctx, `DELETE FROM reg_crypto_aml_reports WHERE id=$1`, report.ID)
	require.ErrorContains(t, err, "append-only")
	_, ok, err = repo.ClaimAMLJob(ctx, now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestAMLCustodyOwnerCannotChangeOrCrossMerchant(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	input, _ := seedAMLJob(t, ctx, pool)
	otherMerchant := id.New()
	_, err := pool.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Other merchant')`, otherMerchant, id.Compact(otherMerchant))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE cat_wallets SET custody_merchant_id=$2 WHERE id=$1`, input.WalletID, otherMerchant)
	require.ErrorContains(t, err, "immutable")
	_, err = pool.Exec(ctx, `UPDATE cat_wallets SET custody_merchant_id=NULL WHERE id=$1`, input.WalletID)
	require.ErrorContains(t, err, "immutable")
	_, err = pool.Exec(ctx, `UPDATE cat_wallets SET merchant_id=$2 WHERE id=$1`, input.WalletID, otherMerchant)
	require.ErrorContains(t, err, "custody boundary")
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(code,name,network_id,merchant_id,custody_merchant_id,address)
		VALUES($1,'Wrong owner',$2,$3,$4,$1)`, id.Compact(id.New()), input.Request.NetworkID, otherMerchant, input.Request.MerchantID)
	require.ErrorContains(t, err, "custody boundary")
	input.Request.MerchantID = otherMerchant
	_, err = NewAMLRepository().EnqueueAMLCheck(ctx, input)
	require.ErrorContains(t, err, "immutable merchant wallet")
}

func TestAMLFoundationEmptyRollbackAndLegacyUpgrade(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	db, err := sql.Open("pgx", pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 60)
	require.NoError(t, err)
	var exists bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='cat_wallets' AND column_name='custody_merchant_id')`).Scan(&exists))
	require.False(t, exists)
	merchantID, networkID, walletID := id.New(), id.New(), id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Legacy AML merchant')`, merchantID, merchantID.String())
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
		VALUES($1,$2,'Legacy AML network','tron',$2,'TRX',6)`, networkID, networkID.String())
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,merchant_id,address)
		VALUES($1,$4,'Legacy shared wallet',$2,$3,$4)`, walletID, networkID, merchantID, walletID.String())
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	var assigned, custody *id.ID
	require.NoError(t, pool.QueryRow(ctx, `SELECT merchant_id,custody_merchant_id FROM cat_wallets WHERE id=$1`, walletID).Scan(&assigned, &custody))
	require.Equal(t, merchantID, *assigned)
	require.Nil(t, custody, "legacy assignment must not be guessed as permanent custody ownership")
	_, err = pool.Exec(ctx, `UPDATE cat_wallets SET custody_merchant_id=$2 WHERE id=$1`, walletID, merchantID)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 60)
	require.ErrorContains(t, err, "cannot roll back AML custody/evidence with data")
	_, err = pool.Exec(ctx, `DELETE FROM cat_wallets WHERE id=$1`, walletID)
	require.ErrorContains(t, err, "cannot be deleted")
}
