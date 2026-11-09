//go:build integration

package crypto_repo

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	dbassets "metapus/db"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

type unusedHistoricalFundingFees struct {
	cryptoapp.NativeFundingFeeSettlement
}

func TestNativeFundingRecognitionUpgradePreservesSettledHistory(t *testing.T) {
	f, target, _ := nativeResourceFixture(t)
	db, err := sql.Open("pgx", f.pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrations, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(f.ctx, 80)
	require.NoError(t, err)
	fee, funding := id.New(), id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_native_fundings(id,basis_type,basis_id,attempt,network_id,native_token_id,source_wallet_id,destination_wallet_id,amount_minor)
  VALUES($1,'CryptoSweepAttempt',$2,1,$3,$4,$5,$6,20)`, funding, id.New(), f.input.Request.NetworkID, f.input.Request.TokenID, f.input.WalletID, target)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_fee_reservations(id,network_id,payer_wallet_id,native_token_id,operation_token_id,reserved_principal_minor,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor)
  VALUES($1,$2,$3,$4,$4,20,'CryptoNativeFunding',$5,'native_funding',1,'native_funding',10)`, fee, f.input.Request.NetworkID, f.input.WalletID, f.input.Request.TokenID, funding)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_native_fundings SET fee_reservation_id=$2 WHERE id=$1`, funding, fee)
	require.NoError(t, err)
	_, err = f.repo.ReserveAMLResourceBudget(f.ctx, resourceRequest(t, f, fee))
	require.NoError(t, err)
	txHash := signResourceFunding(t, f, target, fee, funding)
	err = postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error {
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			SELECT id,'CryptoNativeFunding',1,now(),'expense',source_wallet_id,native_token_id,amount_minor FROM doc_crypto_native_fundings WHERE id=$1
			UNION ALL SELECT id,'CryptoNativeFunding',1,now(),'receipt',destination_wallet_id,native_token_id,amount_minor FROM doc_crypto_native_fundings WHERE id=$1`, funding)
		if err != nil {
			return err
		}
		legacy := f
		legacy.ctx = ctx
		return recordResourceEvidence(legacy, fee, funding, txHash, "succeeded", "3", false)
	})
	require.NoError(t, err)
	var before string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id)::text FROM reg_crypto_balance_movements m WHERE recorder_id=$1`, funding).Scan(&before))
	_, err = provider.Up(f.ctx)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, "UPDATE doc_crypto_native_fundings SET last_error='' WHERE id=$1", funding)
	require.NoError(t, err)
	receipt := crypto.ChainReceipt{NetworkID: f.input.Request.NetworkID, TxHash: txHash, Status: crypto.ReceiptSucceeded,
		BlockHash: "historical-block", BlockHeight: 10, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified,
		ObservedAt: time.Now().UTC(), Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: f.input.Request.TokenID}}}
	settled, err := cryptoapp.NewNativeFundingSettlementService(postgres.NewContextTxManager(), NativeFundingSettlementRepo{}, unusedHistoricalFundingFees{}).Settle(f.ctx, funding, receipt)
	require.NoError(t, err)
	require.True(t, settled)
	var after string
	var claims int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY line_id)::text FROM reg_crypto_balance_movements m WHERE recorder_id=$1`, funding).Scan(&after))
	require.Equal(t, before, after, "upgrade and replay cannot rewrite or repeat historical money")
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT count(*) FROM reg_crypto_native_funding_custody_bindings").Scan(&claims))
	require.Zero(t, claims, "settled history requires explicit backfill, not automatic adoption")
}
