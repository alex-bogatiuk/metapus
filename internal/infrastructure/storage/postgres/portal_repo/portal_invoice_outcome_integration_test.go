//go:build integration

package portal_repo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo/receipttest"
)

func TestPortalInvoiceOutcomeLedgerAndScope(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)
	rollback := errors.New("rollback invoice outcome fixture")
	err = postgres.ReadSnapshot(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		merchantID, invoiceID, walletID, paymentID := id.New(), id.New(), id.New(), id.New()
		networkID, tokenID, code := id.New(), id.New(), id.Compact(merchantID)
		_, err := q.Exec(txCtx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Outcome fixture')`, merchantID, code)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
   VALUES($1,$2,'Outcome network','evm',$2,'ETH',18)`, networkID, "OUT-"+code)
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,contract_address,decimal_places,token_standard)
   VALUES($1,$2,'Outcome token',$3,'OUT',$4,6,'ERC-20')`, tokenID, "OUT-"+code, networkID, "0x"+code[:20]+code[:20])
		require.NoError(t, err)
		_, err = q.Exec(txCtx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id)
   VALUES($1,$2,'Outcome wallet',$3,$4,'outcome-key')`, walletID, "OUT-"+code, networkID, "0x"+code[12:32]+code[12:32])
		require.NoError(t, err)
		// The payment attributes an accounted receipt of its transfer.
		receiptID, _ := receipttest.Record(t, txCtx, receipttest.Deps{Proofs: crypto_repo.NewBlockIngestionRepo(), Repo: crypto_repo.NewReceiptIntakeRepo(), Guard: crypto_repo.NewBlockIngestionRepo()},
			receipttest.Transfer{WalletID: walletID, TokenID: tokenID, TxHash: code + code, Amount: "1000", BlockNumber: 100})
		_, err = q.Exec(txCtx, `INSERT INTO doc_crypto_invoices(id,public_ref,number,date,merchant_id,token_id,expected_amount,received_amount,confirmed_amount,credited_amount,status,expires_at,single_payment,selected_payment_id)
   VALUES($1,$2,'CI-OUTCOME',NOW(),$3,$4,2000,1000,1000,1000,'underpaid',$5,TRUE,$6)`, invoiceID, "ci_"+id.Compact(invoiceID), merchantID, tokenID, time.Now().Add(time.Hour), paymentID)
		require.NoError(t, err)
		insertPayment := func(pid id.ID, amount, fee string, posted bool, version int) {
			txHash, receipt := pid.String(), (*id.ID)(nil)
			if pid == paymentID {
				txHash, receipt = code+code, &receiptID
			}
			_, err := q.Exec(txCtx, `INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,amount,fee_fixed,status,canonicality,posted,posted_version,confirmations,required_confs,receipt_id,event_kind)
   VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'settled','canonical',$10,$11,13,12,$12,'contract_log')`, pid, invoiceID, merchantID, networkID, tokenID, walletID, txHash, amount, fee, posted, version, receipt)
			require.NoError(t, err)
		}
		insertPayment(paymentID, "1000", "100", true, 1)
		for i := 0; i < 60; i++ {
			insertPayment(id.New(), "10", "0", false, 0)
		}
		insertRoots := func(version int) []id.ID {
			lines := []id.ID{id.New(), id.New(), id.New()}
			_, err := q.Exec(txCtx, `INSERT INTO reg_crypto_receipt_suspense_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
    VALUES($1,$2,'CryptoPayment',$3,NOW(),'expense',$4,$5,1000)`, lines[0], paymentID, version, receiptID, tokenID)
			require.NoError(t, err)
			_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
    VALUES($1,$2,'CryptoPayment',$3,NOW(),'receipt',$4,$5,900)`, lines[1], paymentID, version, merchantID, tokenID)
			require.NoError(t, err)
			_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_fee_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount)
    VALUES($1,$2,'CryptoPayment',$3,NOW(),'receipt',$4,$5,'processing',100)`, lines[2], paymentID, version, merchantID, tokenID)
			require.NoError(t, err)
			return lines
		}
		roots := insertRoots(1)
		reverse := func(kind string, amounts []int, reason string) {
			recorder := id.New()
			_, err := q.Exec(txCtx, `INSERT INTO reg_crypto_receipt_suspense_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount,reversal_of_line_id,reversal_reason)
    VALUES($1,$2,$3,1,NOW(),'receipt',$4,$5,$6,$7,$8)`, id.New(), recorder, kind, receiptID, tokenID, amounts[0], roots[0], reason)
			require.NoError(t, err)
			_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount,reversal_of_line_id,reversal_reason)
    VALUES($1,$2,$3,1,NOW(),'expense',$4,$5,$6,$7,$8)`, id.New(), recorder, kind, merchantID, tokenID, amounts[1], roots[1], reason)
			require.NoError(t, err)
			_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_fee_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,fee_type,amount,reversal_of_line_id,reversal_reason)
    VALUES($1,$2,$3,1,NOW(),'expense',$4,$5,'processing',$6,$7,$8)`, id.New(), recorder, kind, merchantID, tokenID, amounts[2], roots[2], reason)
			require.NoError(t, err)
		}
		for i := 0; i < 55; i++ {
			_, err = q.Exec(txCtx, `INSERT INTO sys_webhook_deliveries(invoice_id,merchant_id,event_type,webhook_url,event_id,delivery_id,status_code,response_time_ms)
   VALUES($1,$2,'invoice.underpaid','https://example.test/hook','event',$3,204,0)`, invoiceID, merchantID, id.New().String())
			require.NoError(t, err)
		}
		scope := func(role merchant.PortalRole) context.Context {
			other := id.New()
			return merchant.WithPortalScope(txCtx, merchant.PortalScope{MerchantIDs: []id.ID{merchantID, other}, Roles: map[id.ID]merchant.PortalRole{merchantID: role, other: merchant.PortalRoleManager}})
		}
		repo := NewDashboardRepo()
		read := func(net, fee, reversed string) {
			detail, err := repo.GetInvoiceDetail(scope(merchant.PortalRoleOwner), invoiceID, &merchantID)
			require.NoError(t, err)
			require.Equal(t, "known", detail.FinancialSummary.Ledger.Status)
			require.Equal(t, net, *detail.FinancialSummary.Ledger.CreditedNetAmount)
			require.Equal(t, fee, *detail.FinancialSummary.Ledger.ProcessingFeeAmount)
			require.Equal(t, reversed, *detail.FinancialSummary.Ledger.ReversedNetAmount)
			require.Equal(t, "600", detail.FinancialSummary.PendingNetAmount)
			canonicalGross := "1600"
			if net == "0" {
				canonicalGross = "600"
			}
			require.Equal(t, canonicalGross, detail.ReceivedAmount)
			require.Equal(t, canonicalGross, detail.FinancialSummary.ConfirmedAmount)
			require.Len(t, detail.Payments, 61)
			require.Equal(t, 55, detail.WebhookDeliveryTotal)
			require.Len(t, detail.WebhookDeliveries, 50)
			require.Equal(t, "single_payment_selected", detail.PaymentAcceptanceReason)
		}
		read("900", "100", "0")
		reverse("CryptoAdjustment", []int{120, 100, 20}, "manual_financial_correction")
		read("800", "80", "100")
		reverse("CryptoPaymentReorg", []int{880, 800, 80}, "posted_payment_reorg")
		_, err = q.Exec(txCtx, `UPDATE doc_crypto_payments SET posted=FALSE,canonicality='orphaned',status='reorged' WHERE id=$1`, paymentID)
		require.NoError(t, err)
		read("0", "0", "900")
		insertRoots(2)
		_, err = q.Exec(txCtx, `UPDATE doc_crypto_payments SET posted=TRUE,posted_version=2,canonicality='canonical',status='settled' WHERE id=$1`, paymentID)
		require.NoError(t, err)
		read("900", "100", "900")
		viewer, err := repo.GetInvoiceDetail(scope(merchant.PortalRoleViewer), invoiceID, nil)
		require.NoError(t, err)
		require.False(t, viewer.CanViewWebhooks)
		require.Empty(t, viewer.WebhookDeliveries)
		require.Zero(t, viewer.WebhookDeliveryTotal)
		_, err = repo.GetInvoiceDetail(scope(merchant.PortalRoleOwner), id.New(), &merchantID)
		var appErr *apperror.AppError
		require.ErrorAs(t, err, &appErr)
		require.Equal(t, apperror.CodeNotFound, appErr.Code)
		foreign := id.New()
		foreignCtx := merchant.WithPortalScope(txCtx, merchant.PortalScope{MerchantIDs: []id.ID{foreign}, Roles: map[id.ID]merchant.PortalRole{foreign: merchant.PortalRoleOwner}})
		_, err = repo.GetInvoiceDetail(foreignCtx, invoiceID, nil)
		require.ErrorAs(t, err, &appErr)
		require.Equal(t, apperror.CodeNotFound, appErr.Code)
		insertPayment(id.New(), "1000", "0", true, 1)
		inconsistent, err := repo.GetInvoiceDetail(scope(merchant.PortalRoleOwner), invoiceID, &merchantID)
		require.NoError(t, err)
		require.Equal(t, "inconsistent", inconsistent.FinancialSummary.Ledger.Status)
		require.Nil(t, inconsistent.FinancialSummary.Ledger.CreditedNetAmount)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
