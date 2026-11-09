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

func TestWithdrawalRecognitionUpgradePreservesLegacyMoney(t *testing.T) {
	ctx, pool := amlTestDatabase(t)
	db, err := sql.Open("pgx", pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 83)
	require.NoError(t, err)
	proof := accountingProof(t, ctx, pool)
	asset, wallet, address, request, document, merchant := id.New(), id.New(), id.New(), id.New(), id.New(), id.New()
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,token_standard,contract_address,symbol,decimal_places) VALUES($1,$1::uuid::text,'Legacy asset',$2,'ERC-20',$3,'OLD',6)`, asset, proof.Event.NetworkID, proof.Event.TokenContract)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,tier,wallet_role) VALUES($1,$1::uuid::text,'Legacy withdrawal',$2,$3,'old-key','hot','liquidity_hot')`, wallet, proof.Event.NetworkID, proof.Event.FromAddress)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$1::uuid::text,'Legacy merchant')`, merchant)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_withdrawal_addresses(id,merchant_id,network_id,address,address_key) VALUES($1,$2,$3,$4,lower($4))`, address, merchant, proof.Event.NetworkID, proof.Event.ToAddress)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO doc_withdrawal_requests(id,merchant_id,token_id,address_id,dest_address,amount,posted,posted_version,status) VALUES($1,$2,$3,$4,$5,23,true,1,'broadcast')`, request, merchant, asset, address, proof.Event.ToAddress)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO doc_crypto_withdrawals(id,merchant_id,token_id,source_wallet_id,dest_address,amount,posted,posted_version,status,tx_hash,basis_type,basis_id)
 VALUES($1,$2,$3,$4,$5,23,true,1,'broadcast',$6,'WithdrawalRequest',$7)`, document, merchant, asset, wallet, proof.Event.ToAddress, proof.Event.TxHash, request)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE doc_withdrawal_requests SET withdrawal_id=$2 WHERE id=$1`, request, document)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,'CryptoWithdrawal',1,now(),'expense',$2,$3,23)`, document, wallet, asset)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_merchant_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount) SELECT id,'WithdrawalRequest',1,date,'expense',merchant_id,token_id,amount FROM doc_withdrawal_requests WHERE id=$1`, request)
	require.NoError(t, err)
	snapshot := `SELECT jsonb_build_object('physical',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_balance_movements m WHERE recorder_id=$1),
 'merchant',(SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id) FROM reg_crypto_merchant_balance_movements m WHERE recorder_id=$2))::text`
	var before, after string
	require.NoError(t, pool.QueryRow(ctx, snapshot, document, request).Scan(&before))
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	var model, bindings int
	require.NoError(t, pool.QueryRow(ctx, "SELECT accounting_model_version FROM doc_crypto_withdrawals WHERE id=$1", document).Scan(&model))
	require.Equal(t, 1, model)
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM reg_crypto_withdrawal_custody_bindings").Scan(&bindings))
	require.Zero(t, bindings)
	require.NoError(t, pool.QueryRow(ctx, snapshot, document, request).Scan(&after))
	require.Equal(t, before, after)
	_, err = provider.DownTo(ctx, 83)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO doc_crypto_withdrawals(id,merchant_id,token_id,source_wallet_id,dest_address,amount) VALUES($1,$2,$3,$4,$5,1)`, id.New(), merchant, asset, wallet, proof.Event.ToAddress)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 83)
	require.ErrorContains(t, err, "withdrawal accounting v2 requires forward migration")
}
