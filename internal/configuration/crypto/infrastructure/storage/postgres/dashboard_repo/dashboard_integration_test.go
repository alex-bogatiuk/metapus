//go:build integration

package dashboard_repo

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/tenant"
	"os"
	"testing"
)

// Real schema, temporary tables and a rolled-back transaction. No production
// document, movement, balance or reconciliation observation is modified.
func TestDashboardReadModels(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	rollback := errors.New("rollback dashboard fixtures")
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		exec := func(sql string, args ...any) { _, err := q.Exec(ctx, sql, args...); require.NoError(t, err) }
		for _, table := range []string{"cat_tokens", "cat_blockchain_networks", "cat_merchants", "cat_wallets", "cat_currencies", "doc_crypto_invoices", "doc_crypto_payments", "doc_withdrawal_requests", "reg_crypto_inbound_chain_events", "reg_crypto_external_outflows", "reg_crypto_fee_movements", "reg_crypto_merchant_balance_balances", "reg_crypto_balance_balances", "reg_crypto_reconciliation_checks",
			"doc_crypto_custody_receipts", "reg_crypto_receipt_suspense_balances", "reg_crypto_custody_receipt_audits",
			"reg_crypto_merchant_balance_movements", "reg_crypto_balance_movements", "reg_crypto_withdrawal_custody_bindings"} {
			exec("CREATE TEMP TABLE " + table + " (LIKE public." + table + " INCLUDING DEFAULTS) ON COMMIT DROP")
		}
		for _, table := range []string{"cat_tokens", "cat_blockchain_networks", "cat_merchants", "cat_currencies"} {
			exec("INSERT INTO " + table + " SELECT * FROM public." + table)
		}
		exec(`INSERT INTO cat_wallets SELECT w.* FROM public.cat_wallets w WHERE EXISTS(SELECT 1 FROM cat_tokens t WHERE t.network_id=w.network_id) LIMIT 1`)
		var token, network, wallet, merchant string
		require.NoError(t, q.QueryRow(ctx, `SELECT t.id::text,t.network_id::text,w.id::text FROM cat_tokens t JOIN cat_wallets w ON w.network_id=t.network_id LIMIT 1`).Scan(&token, &network, &wallet))
		require.NoError(t, q.QueryRow(ctx, `SELECT id::text FROM cat_merchants LIMIT 1`).Scan(&merchant))
		exec(`UPDATE cat_tokens SET is_active=FALSE`)
		exec(`UPDATE cat_tokens SET is_active=TRUE WHERE id=$1`, token)
		exec(`UPDATE cat_wallets SET is_active=TRUE,deletion_mark=FALSE,_deleted_at=NULL`)
		exec(`INSERT INTO doc_crypto_invoices(public_ref,number,date,merchant_id,token_id,expected_amount,expires_at,status)
   SELECT 'dashboard-'||n,'CI-test-'||n,NOW(),$1,$2,1000000,NOW()+INTERVAL '1 hour','completed' FROM generate_series(1,60) n`, merchant, token)
		exec(`INSERT INTO doc_crypto_invoices(public_ref,number,date,merchant_id,token_id,expected_amount,expires_at,status,created_at)
   VALUES('old-test','CI-old',NOW(),$1,$2,1000000,NOW(),'paid',NOW()-INTERVAL '20 days')`, merchant, token)
		exec(`INSERT INTO doc_withdrawal_requests(id,number,date,merchant_id,token_id,amount,dest_address,status)
   SELECT gen_random_uuid_v7(),'WR-test-'||n,NOW(),$1,$2,1000000,'destination','pending_approval' FROM generate_series(1,61) n`, merchant, token)
		repo := New()
		queues, err := repo.Queues(ctx, "withdrawal", 5)
		require.NoError(t, err)
		require.Equal(t, 61, queues.Withdrawals)
		require.Equal(t, 61, queues.Total)
		require.Len(t, queues.Items, 5)
		empty, err := repo.Queues(ctx, "inbound", 5)
		require.NoError(t, err)
		require.Empty(t, empty.Items)
		require.Equal(t, 61, empty.Total)
		// A same-symbol token on another network must never share aggregates.
		exec(`INSERT INTO cat_tokens SELECT * FROM cat_tokens WHERE id=$1`, token)
		var second string
		require.NoError(t, q.QueryRow(ctx, `SELECT gen_random_uuid_v7()::text`).Scan(&second))
		exec(`UPDATE cat_tokens SET id=$2,network_id=(SELECT id FROM cat_blockchain_networks WHERE id<>$3 LIMIT 1) WHERE ctid=(SELECT MAX(ctid) FROM cat_tokens WHERE id=$1)`, token, second, network)
		exec(`INSERT INTO reg_crypto_merchant_balance_balances(merchant_id,token_id,amount) VALUES($1,$2,900719925474099312345678901)`, merchant, token)
		exec(`INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,900719925474099312345678902)`, wallet, token)
		// Payouts still in flight: one request is half paid, one was refunded in
		// full, and a failed request is terminal, so its remainder is not owed.
		var halfPaid, refunded, failed, payout string
		require.NoError(t, q.QueryRow(ctx, `SELECT gen_random_uuid_v7()::text,gen_random_uuid_v7()::text,gen_random_uuid_v7()::text,gen_random_uuid_v7()::text`).Scan(&halfPaid, &refunded, &failed, &payout))
		exec(`INSERT INTO doc_withdrawal_requests(id,number,date,merchant_id,token_id,amount,dest_address,status)
   VALUES($1,'WR-half',NOW(),$4,$5,500,'destination','broadcast'),($2,'WR-refunded',NOW(),$4,$5,50,'destination','signing'),($3,'WR-failed',NOW(),$4,$5,100,'destination','failed')`, halfPaid, refunded, failed, merchant, token)
		exec(`INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
   VALUES(gen_random_uuid_v7(),$1,'WithdrawalRequest',1,NOW(),'expense',$4,$5,500),
   (gen_random_uuid_v7(),$2,'WithdrawalRequest',1,NOW(),'expense',$4,$5,50),(gen_random_uuid_v7(),$2,'WithdrawalRequest',2,NOW(),'receipt',$4,$5,50),
   (gen_random_uuid_v7(),$3,'WithdrawalRequest',1,NOW(),'expense',$4,$5,100)`, halfPaid, refunded, failed, merchant, token)
		exec(`INSERT INTO reg_crypto_withdrawal_custody_bindings(posting_id,withdrawal_id,request_id,signing_authorization_id,merchant_debit_line_id,period)
   VALUES(gen_random_uuid_v7(),$1,$2,gen_random_uuid_v7(),gen_random_uuid_v7(),NOW())`, payout, halfPaid)
		exec(`INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
   VALUES(gen_random_uuid_v7(),$1,'CryptoWithdrawal',1,NOW(),'expense',$2,$3,200)`, payout, wallet, token)
		assets, err := repo.Assets(ctx)
		require.NoError(t, err)
		for _, a := range assets {
			if a.TokenID == token {
				require.Equal(t, "900719925474099312345678901", a.MerchantMinor)
				require.Equal(t, "300", a.PayoutPendingMinor)
				require.Equal(t, 1, a.PayoutPendingCount)
				require.Equal(t, "900719925474099312345679201", a.ObligationMinor)
				require.Nil(t, a.ChainMinor)
				require.Equal(t, 1, a.WalletCount)
			}
		}
		// Opposite per-wallet deltas must remain visible even when their net sum is zero.
		exec(`INSERT INTO cat_wallets SELECT * FROM cat_wallets LIMIT 1`)
		var wallet2 string
		require.NoError(t, q.QueryRow(ctx, `SELECT gen_random_uuid_v7()::text`).Scan(&wallet2))
		exec(`UPDATE cat_wallets SET id=$2 WHERE ctid=(SELECT MAX(ctid) FROM cat_wallets WHERE id=$1)`, wallet, wallet2)
		exec(`INSERT INTO reg_crypto_reconciliation_checks(run_id,network_id,wallet_id,token_id,ledger_amount,chain_amount,delta_amount)
   VALUES(gen_random_uuid_v7(),$1,$2,$3,100,110,10),(gen_random_uuid_v7(),$1,$4,$3,100,90,-10)`, network, wallet, token, wallet2)
		assets, err = repo.Assets(ctx)
		require.NoError(t, err)
		for _, a := range assets {
			if a.TokenID == token {
				require.NotNil(t, a.DeltaMinor)
				require.Equal(t, "0", *a.DeltaMinor)
				require.Equal(t, 2, a.MismatchedWalletCount)
			}
		}
		exec(`INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,amount,status,posted,confirmed_at)
   SELECT gen_random_uuid_v7(),(SELECT id FROM doc_crypto_invoices LIMIT 1),$1,$2,$3,$4,'dashboard-'||n,1000000000000000000000,'confirmed',TRUE,NOW() FROM generate_series(1,60) n`, merchant, network, token, wallet)
		exec(`INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,amount,status,posted,confirmed_at,canonicality)
   VALUES(gen_random_uuid_v7(),(SELECT id FROM doc_crypto_invoices LIMIT 1),$1,$2,$3,$4,'unposted',999,'confirmed',FALSE,NOW(),'canonical'),
   (gen_random_uuid_v7(),(SELECT id FROM doc_crypto_invoices LIMIT 1),$1,$2,$3,$4,'orphaned',999,'confirmed',TRUE,NOW(),'orphaned')`, merchant, network, token, wallet)
		exec(`INSERT INTO reg_crypto_fee_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount)
   VALUES(gen_random_uuid_v7(),gen_random_uuid_v7(),'CryptoPayment',1,NOW(),'receipt',$1,$2,'processing',100),
   (gen_random_uuid_v7(),gen_random_uuid_v7(),'CryptoPayment',1,NOW(),'expense',$1,$2,'processing',20),
   (gen_random_uuid_v7(),gen_random_uuid_v7(),'TreasuryWithdrawal',1,NOW(),'expense',$1,$2,'processing',40),
   (gen_random_uuid_v7(),gen_random_uuid_v7(),'CryptoPayment',1,NOW(),'receipt',$1,$2,'network',999)`, merchant, token)
		exec(`UPDATE cat_tokens SET is_active=FALSE,_deleted_at=NOW() WHERE id=$1`, token)
		archivedAssets, err := repo.Assets(ctx)
		require.NoError(t, err)
		found := false
		for _, a := range archivedAssets {
			if a.TokenID == token {
				found = true
				require.Equal(t, "900719925474099312345678901", a.MerchantMinor)
			}
		}
		require.True(t, found, "archived tokens with financial balances must remain visible")
		exec(`INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,amount,status,posted,confirmed_at)
         VALUES(gen_random_uuid_v7(),(SELECT id FROM doc_crypto_invoices LIMIT 1),$1,(SELECT network_id FROM cat_tokens WHERE id=$2),$2,$3,'other-network',3,'confirmed',TRUE,NOW())`, merchant, second, wallet)

		for _, period := range []string{"7d", "30d", "90d"} {
			analytics, err := repo.Analytics(ctx, period)
			require.NoError(t, err)
			require.Equal(t, period, analytics.Period)
			require.Len(t, analytics.Receipts, 2)
			for _, point := range analytics.Receipts {
				if point.TokenID == token {
					require.Equal(t, "60000000000000000000000", point.Deposits)
				} else {
					require.Equal(t, second, point.TokenID)
					require.Equal(t, "3", point.Deposits)
				}
			}
			require.Len(t, analytics.Assets, 2)
			for _, a := range analytics.Assets {
				switch a.TokenID {
				case token:
					require.Equal(t, "80", a.FeesMinor)
					require.Equal(t, 60, a.Paid)
					if period == "7d" {
						require.Equal(t, 60, a.Created)
						require.Zero(t, a.AwaitingConfirmation)
					} else {
						require.Equal(t, 61, a.Created)
						require.Equal(t, 1, a.AwaitingConfirmation)
					}
				case second:
					require.Zero(t, a.Created)
					require.Equal(t, "0", a.FeesMinor)
				}
			}
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
