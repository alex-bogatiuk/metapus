//go:build integration

package portal_repo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/tenant"
)

// A merchant refund is a withdrawal request based on a credited payment. The
// database caps all refunds of one payment by its gross, counting net request
// debits, so a rejected refund frees its amount again.
func TestMerchantRefundIsCappedByThePayment(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	rollback := errors.New("rollback merchant refund fixture")
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		var tokenID, networkID id.ID
		err := q.QueryRow(ctx, `SELECT id, network_id FROM cat_tokens WHERE _deleted_at IS NULL AND deletion_mark = FALSE ORDER BY created_at LIMIT 1`).Scan(&tokenID, &networkID)
		if errors.Is(err, pgx.ErrNoRows) {
			t.Skip("test database has no crypto token")
		}
		require.NoError(t, err)
		merchantID, otherMerchant, invoiceID, paymentID, pendingPayment := id.New(), id.New(), id.New(), id.New(), id.New()
		exec := func(sql string, args ...any) {
			t.Helper()
			_, err := q.Exec(ctx, sql, args...)
			require.NoError(t, err)
		}
		exec(`INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Refund merchant'),($3,$4,'Other merchant')`,
			merchantID, "rf-"+id.Compact(merchantID), otherMerchant, "rf-"+id.Compact(otherMerchant))
		exec(`INSERT INTO doc_crypto_invoices(id,public_ref,number,date,merchant_id,token_id,expected_amount,status,expires_at)
			VALUES($1,$2,'CI-REFUND',NOW(),$3,$4,1000,'completed',$5)`, invoiceID, "ci_"+id.Compact(invoiceID), merchantID, tokenID, time.Now().Add(time.Hour))
		exec(`INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,from_address,amount,status,canonicality,posted)
			VALUES($1,$3,$4,$5,$6,gen_random_uuid_v7(),$7,'payer',1000,'settled','canonical',TRUE),
			      ($2,$3,$4,$5,$6,gen_random_uuid_v7(),$8,'payer',500,'confirming','canonical',FALSE)`,
			paymentID, pendingPayment, invoiceID, merchantID, networkID, tokenID, id.New().String(), id.New().String())
		exec(`INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
			VALUES(gen_random_uuid_v7(),gen_random_uuid_v7(),'Opening',1,NOW(),'receipt',$1,$2,10000)`, merchantID, tokenID)

		// insertRefund posts the request debit first, as the posting engine does,
		// then the request row, whose guard checks the cap. It runs in a
		// savepoint so a refused refund leaves the fixture usable.
		insertRefund := func(merchant, payment id.ID, amount int64, reason string) (id.ID, error) {
			request := id.New()
			err := txm.RunInSavepoint(ctx, func(ctx context.Context) error {
				q := txm.GetQuerier(ctx)
				if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
					VALUES(gen_random_uuid_v7(),$1,'WithdrawalRequest',1,NOW(),'expense',$2,$3,$4)`, request, merchant, tokenID, amount); err != nil {
					return err
				}
				_, err := q.Exec(ctx, `INSERT INTO doc_withdrawal_requests(id,number,date,merchant_id,token_id,amount,dest_address,status,posted,posted_version,basis_type,basis_id,description)
					VALUES($1,$2,NOW(),$3,$4,$5,'payer-confirmed','pending_signature',TRUE,1,'CryptoPayment',$6,$7)`,
					request, "WR-"+id.Compact(request), merchant, tokenID, amount, payment, reason)
				return err
			})
			return request, err
		}

		repo := NewDashboardRepo()
		first, err := insertRefund(merchantID, paymentID, 600, "Order cancelled")
		require.NoError(t, err)
		payment, err := repo.GetRefundablePayment(ctx, merchantID, paymentID)
		require.NoError(t, err)
		require.Equal(t, "600", payment.Committed.String())
		require.Equal(t, "400", payment.Refundable().String())
		require.Equal(t, "CI-REFUND", payment.InvoiceNumber)
		require.Equal(t, "payer", payment.FromAddress)

		_, err = insertRefund(merchantID, paymentID, 500, "Second refund")
		require.ErrorContains(t, err, "merchant refund exceeds the payment amount")
		_, err = insertRefund(merchantID, paymentID, 100, " ")
		require.ErrorContains(t, err, "merchant refund requires its reason")
		_, err = insertRefund(otherMerchant, paymentID, 100, "Not mine")
		require.ErrorContains(t, err, "credited payment of the same merchant")
		_, err = insertRefund(merchantID, pendingPayment, 100, "Not credited yet")
		require.ErrorContains(t, err, "credited payment of the same merchant")
		_, err = repo.GetRefundablePayment(ctx, merchantID, pendingPayment)
		require.Error(t, err, "an uncredited payment is not refundable")

		// Rejecting the first refund restores its debit and frees its amount.
		exec(`INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
			VALUES(gen_random_uuid_v7(),$1,'WithdrawalRequest',2,NOW(),'receipt',$2,$3,600)`, first, merchantID, tokenID)
		exec(`UPDATE doc_withdrawal_requests SET status='rejected',rejection_reason='Changed mind' WHERE id=$1`, first)
		_, err = insertRefund(merchantID, paymentID, 1000, "Full refund")
		require.NoError(t, err)
		payment, err = repo.GetRefundablePayment(ctx, merchantID, paymentID)
		require.NoError(t, err)
		require.Equal(t, "0", payment.Refundable().String())

		// The basis and the reason of a refund cannot be rewritten afterwards.
		err = txm.RunInSavepoint(ctx, func(ctx context.Context) error {
			_, err := txm.GetQuerier(ctx).Exec(ctx, `UPDATE doc_withdrawal_requests SET description='Other reason' WHERE id=$1`, first)
			return err
		})
		require.ErrorContains(t, err, "withdrawal request basis is immutable")

		// The invoice page reports what is held and what is left to refund.
		summaries, err := repo.loadInvoicePayments(ctx, []id.ID{invoiceID}, true)
		require.NoError(t, err)
		for _, p := range summaries[invoiceID.String()].Payments {
			if p.ID == paymentID.String() {
				require.Equal(t, "0", p.RefundedAmount)
				require.Equal(t, "1000", p.RefundPendingAmount)
				require.Equal(t, "0", p.RefundableAmount)
			} else {
				require.Equal(t, "0", p.RefundableAmount, "an uncredited payment offers no refund")
			}
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
