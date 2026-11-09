//go:build integration

package crypto_repo

import (
	"database/sql"
	"io/fs"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	dbassets "metapus/db"
	"metapus/internal/core/id"
)

func TestTreasuryRecognitionUpgradePreservesLegacyMoney(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	db, err := sql.Open("pgx", pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 82)
	require.NoError(t, err)
	proof := accountingProof(t, ctx, pool)
	token, wallet, address, document, merchant := id.New(), id.New(), id.New(), id.New(), id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,token_standard,contract_address,symbol,decimal_places) VALUES($1,$1::uuid::text,'Legacy asset',$2,'ERC-20',$3,'OLD',6)`, token, proof.Event.NetworkID, proof.Event.TokenContract)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,tier,wallet_role) VALUES($1,$1::uuid::text,'Legacy treasury',$2,$3,'old-key','hot','treasury')`, wallet, proof.Event.NetworkID, proof.Event.FromAddress)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Legacy revenue')`, merchant)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_treasury_withdrawal_addresses(id,network_id,address,address_key) VALUES($1,$2,$3,lower($3))`, address, proof.Event.NetworkID, proof.Event.ToAddress)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO doc_treasury_withdrawals(id,token_id,source_wallet_id,address_id,dest_address,amount,posted,posted_version,status,tx_hash)
		VALUES($1,$2,$3,$4,$5,23,true,1,'broadcast',$6)`, document, token, wallet, address, proof.Event.ToAddress, proof.Event.TxHash)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
		VALUES($1,'TreasuryWithdrawal',1,now(),'expense',$2,$3,23)`, document, wallet, token)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_fee_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount)
		VALUES(gen_random_uuid_v7(),$1,'TreasuryWithdrawal',1,now(),'expense',$2,$3,'processing',23)`, document, merchant, token)
	require.NoError(t, err)
	var before, after string
	snapshot := `SELECT jsonb_build_object('physical',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_balance_movements m WHERE recorder_id=$1),
		'revenue',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_fee_movements m WHERE recorder_id=$1))::text`
	require.NoError(t, pool.QueryRow(ctx, snapshot, document).Scan(&before))
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	var model, claims int
	require.NoError(t, pool.QueryRow(ctx, "SELECT accounting_model_version FROM doc_treasury_withdrawals WHERE id=$1", document).Scan(&model))
	require.Equal(t, 1, model)
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM reg_crypto_treasury_custody_bindings").Scan(&claims))
	require.Zero(t, claims)
	require.NoError(t, pool.QueryRow(ctx, snapshot, document).Scan(&after))
	require.Equal(t, before, after)
	_, err = provider.DownTo(ctx, 82)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO doc_treasury_withdrawals(id,token_id,source_wallet_id,address_id,dest_address,amount) VALUES($1,$2,$3,$4,$5,1)`, id.New(), token, wallet, address, proof.Event.ToAddress)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 82)
	require.ErrorContains(t, err, "treasury accounting v2 requires forward migration")
}
