//go:build integration

package crypto_repo

import (
	"context"
	"database/sql"
	"io/fs"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	dbassets "metapus/db"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestNetworkFeeUpgradePreservesLegacyMoneyAndGuardsRollback(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	db, err := sql.Open("pgx", pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 85)
	require.NoError(t, err)
	network, asset, wallet, fee, basis := id.New(), id.New(), id.New(), id.New(), id.New()
	hash := strings.Repeat("a", 64)
	_, err = pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES($1,$1::uuid::text,'Fee history','evm',$1::uuid::text,'ETH',18)`, network)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard) VALUES($1,$1::uuid::text,'Native',$2,'ETH',18,'native')`, asset, network)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id) VALUES($1,$1::uuid::text,'Payer',$2,'0x00000000000000000000000000000000000000ab','source-key')`, wallet, network)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,status,tx_hash) VALUES($1,$2,$3,$4,$4,'FeeHistory',$5,'sweep',1,'native_burn',10,'broadcast',$6)`, fee, network, wallet, asset, basis, hash)
	require.NoError(t, err)
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_chain_settlements(network_id,basis_type,basis_id,tx_hash,status,block_hash,block_height,finality_status,native_token_id,total_fee_minor,raw_receipt,observed_at) VALUES($1,'FeeHistory',$2,$3,'reverted',repeat('b',64),100,'solidified',$4,2,'{}',now())`, network, basis, hash, asset); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,$2,'ChainSettlement',1,now(),'expense',$3,$4,2)`, fee, basis, wallet, asset); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_platform_cost_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount) VALUES($1,$2,'ChainSettlement',1,now(),'expense',$3,$4,$4,'chain_network_fee','sweep','chain',$5,2)`, fee, basis, network, asset, hash); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_fee_reservation_events(reservation_id,from_status,to_status,amount_minor) VALUES($1,'broadcast','settled',2)`, fee); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `UPDATE reg_crypto_fee_reservations SET status='settled',actual_fee_minor=2,settled_at=now() WHERE id=$1`, fee)
		return err
	})
	require.NoError(t, err)
	snapshot := `SELECT jsonb_build_object('physical',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_balance_movements m WHERE line_id=$1),'cost',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_platform_cost_movements m WHERE line_id=$1),'events',(SELECT jsonb_agg(to_jsonb(e)-'transition_txid' ORDER BY id) FROM reg_crypto_fee_reservation_events e WHERE reservation_id=$1))::text`
	var before, after string
	require.NoError(t, pool.QueryRow(ctx, snapshot, fee).Scan(&before))
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, snapshot, fee).Scan(&after))
	require.Equal(t, before, after)
	var model, claims int
	require.NoError(t, pool.QueryRow(ctx, `SELECT accounting_model_version FROM reg_crypto_fee_reservations WHERE id=$1`, fee).Scan(&model))
	require.Equal(t, 1, model)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_network_fee_bindings`).Scan(&claims))
	require.Zero(t, claims)
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_chain_settlements(network_id,basis_type,basis_id,tx_hash,status,block_hash,block_height,finality_status,native_token_id,total_fee_minor,observed_at,network_fee_reservation_id,raw_receipt)
 VALUES($1,'InvalidLegacyFeeOwner',$2,repeat('d',64),'reverted',repeat('b',64),100,'solidified',$3,2,now(),$4,'{}')`, network, id.New(), asset, fee)
	require.ErrorContains(t, err, "network fee recognition requires model 2", "legacy fee cannot acquire a new recognition reference")
	var key string
	require.NoError(t, pool.QueryRow(ctx, `SELECT fn_crypto_network_fee_key('evm','aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','0x'||repeat('1',64))`).Scan(&key))
	require.Equal(t, "07a96a578c2cdae696c144ead6d0d652dc99799283e05762c25056814c46c805", key)
	_, err = provider.DownTo(ctx, 85)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor) VALUES($1,$2,$3,$4,$4,'FeeHistory',$5,'sweep',1,'native_burn',10)`, id.New(), network, wallet, asset, id.New())
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 85)
	require.ErrorContains(t, err, "network fee recognition v2 requires forward migration")
}
