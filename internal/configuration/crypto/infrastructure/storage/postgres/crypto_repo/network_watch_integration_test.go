//go:build integration

package crypto_repo

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/tenant"
)

type networkWatchFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	network id.ID
	token   id.ID
}

func newNetworkWatchFixture(t *testing.T) networkWatchFixture {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	ctx = tenant.WithTxManager(ctx, sdktest.NewTxManager(pool))
	f := networkWatchFixture{ctx: ctx, pool: pool, network: id.New(), token: id.New()}
	suffix := id.Compact(f.network)
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,is_active)
		VALUES($1,$2,'Network watch','evm',$3,'ETH',18,TRUE);
		INSERT INTO cat_tokens(id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
		VALUES($4,$5,'Network watch native',$1,'','ETH',18,'native')`,
		pgx.QueryExecModeSimpleProtocol, f.network, "NW-"+suffix, "nw-"+suffix, f.token, "NW-ETH-"+suffix)
	require.NoError(t, err)
	return f
}

func (f networkWatchFixture) setWatching(watching bool) error {
	_, err := NewNetworkPolicyRepo().SetNetworkWatching(f.ctx, f.network, watching)
	return err
}

func (f networkWatchFixture) watching(t *testing.T) bool {
	t.Helper()
	var active bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT is_active FROM cat_blockchain_networks WHERE id=$1`, f.network).Scan(&active))
	return active
}

func requireWatchRefusal(t *testing.T, err error, reason string) {
	t.Helper()
	require.ErrorContains(t, err, reason)
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok && appErr.Code == apperror.CodeValidation, "a guard refusal is a validation error: %v", err)
}

func TestNetworkWatchingStopsOnlyWhenNothingNeedsTheChain(t *testing.T) {
	f := newNetworkWatchFixture(t)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=1000 WHERE network_id=$1`, f.network)
	require.NoError(t, err)
	requireWatchRefusal(t, f.setWatching(false), "operations processing is enabled")
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=FALSE WHERE network_id=$1`, f.network)
	require.NoError(t, err)

	merchant, invoice := id.New(), id.New()
	_, err = f.pool.Exec(f.ctx, `
		INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Network watch merchant');
		INSERT INTO doc_crypto_invoices(id,public_ref,number,date,merchant_id,token_id,expected_amount,status,expires_at,ttl_minutes,creation_source)
		VALUES($3,$4,$5,NOW(),$1,$6,100,'paid',NOW()+interval '1 hour',60,'portal')`,
		pgx.QueryExecModeSimpleProtocol, merchant, "NW-M-"+id.Compact(merchant), invoice, "ci_"+id.Compact(invoice), "NW-"+id.Compact(invoice)[:8], f.token)
	require.NoError(t, err)
	requireWatchRefusal(t, f.setWatching(false), "invoices await payment or finality")
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_invoices SET status='completed' WHERE id=$1`, invoice)
	require.NoError(t, err)

	authorization := id.New()
	_, err = f.pool.Exec(f.ctx, `
		INSERT INTO reg_crypto_signing_authorizations(id,authorization_id,tenant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,
			source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,amount_minor_units,basis_type,basis_id,expires_at,policy_version,
			authorization_jws,unsigned_fingerprint,unsigned_envelope)
		VALUES($1,$1::text,'tenant','sweep',1,$2,'NW','evm','11155111','source','source','test/key','destination','native',1,'CryptoSweepAttempt',$3,
			NOW()+interval '1 hour','evm-v1','jws','fingerprint-'||$1::text,'{}')`,
		pgx.QueryExecModeSimpleProtocol, authorization, f.network, id.New())
	require.NoError(t, err)
	requireWatchRefusal(t, f.setWatching(false), "outbound transactions await the chain")
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_signing_authorizations SET status='failed' WHERE id=$1`, authorization)
	require.NoError(t, err)

	// A broadcast transaction keeps its authorization in 'broadcast' after it
	// settles, so only the settlement of its hash releases the network.
	sent, basis := id.New(), id.New()
	txHash := "0x" + id.Compact(sent)
	_, err = f.pool.Exec(f.ctx, `
		INSERT INTO reg_crypto_signing_authorizations(id,authorization_id,tenant_id,purpose,schema_version,network_id,network_code,chain_family,network_identifier,
			source_address,fee_payer_address,vault_key_id,destination_address,asset_standard,amount_minor_units,basis_type,basis_id,expires_at,policy_version,
			authorization_jws,unsigned_fingerprint,unsigned_envelope,status,tx_hash)
		VALUES($1,$1::text,'tenant','sweep',1,$2,'NW','evm','11155111','source','source','test/key','destination','native',1,'CryptoSweepAttempt',$3,
			NOW()+interval '1 hour','evm-v1','jws','fingerprint-'||$1::text,'{}','broadcast',$4)`,
		pgx.QueryExecModeSimpleProtocol, sent, f.network, basis, txHash)
	require.NoError(t, err)
	requireWatchRefusal(t, f.setWatching(false), "outbound transactions await the chain")

	// The settlement alone is the fixture: its fee proof chain is not under test.
	tx, err := f.pool.Begin(f.ctx)
	require.NoError(t, err)
	_, err = tx.Exec(f.ctx, `SET LOCAL session_replication_role = replica`)
	require.NoError(t, err)
	_, err = tx.Exec(f.ctx, `
		INSERT INTO reg_crypto_chain_settlements(network_id,basis_type,basis_id,tx_hash,status,block_hash,block_height,finality_status,native_token_id,total_fee_minor,raw_receipt,observed_at,network_fee_reservation_id)
		VALUES($1,'CryptoSweepAttempt',$2,$3,'succeeded','0xblock',1,'finalized',$4,0,'{}',NOW(),$5)`,
		f.network, basis, txHash, f.token, id.New())
	require.NoError(t, err)
	require.NoError(t, tx.Commit(f.ctx))

	state, err := NewNetworkPolicyRepo().SetNetworkWatching(f.ctx, f.network, false)
	require.NoError(t, err)
	require.False(t, state.Watching, "an unwatched network stays visible to the networks page")
	require.False(t, f.watching(t))

	require.NoError(t, f.setWatching(true), "watching resumes without preconditions")
	require.True(t, f.watching(t))
}

func TestOperationsProcessingRequiresAWatchedNetwork(t *testing.T) {
	f := newNetworkWatchFixture(t)
	require.NoError(t, f.setWatching(false))
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=1000 WHERE network_id=$1`, f.network)
	require.ErrorContains(t, err, "operations processing requires a watched network")

	require.NoError(t, f.setWatching(true))
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,per_transaction_max_fee_minor=10,daily_network_fee_budget_minor=1000 WHERE network_id=$1`, f.network)
	require.NoError(t, err)
}
