//go:build integration

package crypto_repo

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	dbassets "metapus/db"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

func TestCustodyLossUpgradePreservesLegacyMoneyAndGuardsRollback(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	db, err := sql.Open("pgx", pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 84)
	require.NoError(t, err)
	proof := accountingProof(t, ctx, pool)
	asset, wallet, outflow, doc, maker, checker := id.New(), id.New(), id.New(), id.New(), id.New(), id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,token_standard,contract_address,symbol,decimal_places) VALUES($1,$1::uuid::text,'Legacy loss',$2,'ERC-20',$3,'LOSS',6)`, asset, proof.Event.NetworkID, proof.Event.TokenContract)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id) VALUES($1,$1::uuid::text,'Legacy source',$2,$3,'legacy-key')`, wallet, proof.Event.NetworkID, proof.Event.FromAddress)
	require.NoError(t, err)
	e := proof.Event
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_external_outflows(id,network_id,token_id,source_wallet_id,tx_hash,event_kind,event_index,from_address,to_address,contract_address,source_address_key,destination_address_key,contract_address_key,amount,block_number,block_hash,confirmations,required_confs,finality_status)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,lower($8),lower($9),lower($10),23,100,$11,20,19,'finalized')`, outflow, e.NetworkID, asset, wallet, e.TxHash, e.EventKind, e.EventIndex, e.FromAddress, e.ToAddress, e.TokenContract, e.BlockHash)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE reg_crypto_external_outflows SET status='classified',classification='unauthorized_loss',classified_by=$2,classified_at=now(),classification_reason='legacy loss' WHERE id=$1`, outflow, maker)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO doc_crypto_custody_adjustments(id,external_outflow_id,network_id,wallet_id,token_id,amount,maker_id) VALUES($1,$2,$3,$4,$5,23,$6)`, doc, outflow, e.NetworkID, wallet, asset, maker)
	require.NoError(t, err)
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,'CryptoCustodyAdjustment',1,now(),'expense',$2,$3,23)`, doc, wallet, asset); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_platform_cost_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,network_id,operation_token_id,cost_token_id,cost_type,purpose,provider,external_order_id,amount) VALUES(gen_random_uuid_v7(),$1,'CryptoCustodyAdjustment',1,now(),'expense',$2,$3,$3,'custody_loss','custody_loss','custody',$4,23)`, doc, e.NetworkID, asset, outflow.String()); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `UPDATE doc_crypto_custody_adjustments SET status='posted',posted_version=1,decision_by=$2,decision_reason='legacy checker',decided_at=now() WHERE id=$1`, doc, checker)
		return err
	})
	require.NoError(t, err)
	snapshot := `SELECT jsonb_build_object('physical',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_balance_movements m WHERE recorder_id=$1),'cost',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_platform_cost_movements m WHERE recorder_id=$1))::text`
	var before, after string
	require.NoError(t, pool.QueryRow(ctx, snapshot, doc).Scan(&before))
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	var model, claims int
	require.NoError(t, pool.QueryRow(ctx, `SELECT accounting_model_version FROM doc_crypto_custody_adjustments WHERE id=$1`, doc).Scan(&model))
	require.Equal(t, 1, model)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_custody_loss_bindings`).Scan(&claims))
	require.Zero(t, claims)
	require.NoError(t, pool.QueryRow(ctx, snapshot, doc).Scan(&after))
	require.Equal(t, before, after)
	_, err = provider.DownTo(ctx, 84)
	require.NoError(t, err)
	// Exercise the original v2 rollback guard at its own schema version;
	// newer source-history guards may intentionally reject an earlier Down.
	_, err = provider.UpTo(ctx, 85)
	require.NoError(t, err)
	newOutflow := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_external_outflows(id,network_id,token_id,source_wallet_id,tx_hash,event_kind,event_index,from_address,to_address,contract_address,source_address_key,destination_address_key,contract_address_key,amount)
 SELECT $2,network_id,token_id,source_wallet_id,tx_hash,event_kind,event_index+1,from_address,to_address,contract_address,source_address_key,destination_address_key,contract_address_key,amount FROM reg_crypto_external_outflows WHERE id=$1`, outflow, newOutflow)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO doc_crypto_custody_adjustments(external_outflow_id,network_id,wallet_id,token_id,amount,maker_id) VALUES($1,$2,$3,$4,23,$5)`, newOutflow, e.NetworkID, wallet, asset, maker)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 84)
	require.ErrorContains(t, err, "custody loss recognition v2 requires forward migration")
}
