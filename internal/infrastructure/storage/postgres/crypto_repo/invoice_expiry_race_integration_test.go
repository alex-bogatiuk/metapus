//go:build integration

package crypto_repo

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo/receipttest"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

func TestInvoiceExpiryAgainstFullPaymentPreservesCommittedWinnerAndCredit(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	networkID, tokenID, merchantID, walletID, invoiceID, paymentID := id.New(), id.New(), id.New(), id.New(), id.New(), id.New()
	suffix := id.Compact(invoiceID)
	now := time.Now().UTC()
	publicRef := "ci_" + strings.ReplaceAll(invoiceID.String(), "-", "")
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_blockchain_networks(
			id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,is_active)
		VALUES($1,$2,'Invoice expiry race network','evm',$3,'ETH',18,FALSE);
		INSERT INTO cat_merchants(id,code,name)
		VALUES($4,$5,'Invoice expiry race merchant');
		INSERT INTO cat_tokens(
			id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
		VALUES($6,$7,'Invoice expiry race token',$1,$8,$9,6,'TRC-20');
		INSERT INTO cat_wallets(
			id,code,name,network_id,address,vault_key_id,tier,wallet_role,status,
			allocation_mode,is_active,chain_activation_status,leased_for_id,leased_until)
		VALUES($10,$11,'Invoice expiry race wallet',$1,$12,'expiry-key','pool','merchant_pool','leased',
		       'transient',TRUE,'active',$13,$14);
		INSERT INTO doc_crypto_invoices(
			id,public_ref,number,date,merchant_id,token_id,wallet_id,expected_amount,
			status,expires_at,ttl_minutes,creation_source)
		VALUES($13,$15,$16,$14::timestamptz,$4,$6,$10,100,'created',
		       $14::timestamptz-interval '1 minute',5,'portal')
	`, pgx.QueryExecModeSimpleProtocol, networkID, "IER-NET-"+suffix, "ier-"+suffix,
		merchantID, "IER-MERCHANT-"+suffix,
		tokenID, "IER-TOKEN-"+suffix, "0x"+suffix[:20]+suffix[:20], "ER"+suffix[:6],
		walletID, "IER-WALLET-"+suffix, "0x"+suffix[len(suffix)-20:]+suffix[len(suffix)-20:],
		invoiceID, now, publicRef, "EXPIRY-RACE-"+suffix)
	require.NoError(t, err)

	// The transfer is already accounted as a receipt; the racing payment
	// attributes it to the merchant.
	txHash := suffix + suffix
	receiptID, _ := receipttest.Record(t, ctx, receipttest.Deps{Proofs: NewBlockIngestionRepo(), Repo: NewReceiptIntakeRepo(), Guard: NewBlockIngestionRepo()},
		receipttest.Transfer{WalletID: walletID, TokenID: tokenID, TxHash: txHash, Amount: "100", BlockNumber: 100, BlockHash: strings.Repeat("e", 64)})

	invoiceRepo := document_repo.NewCryptoInvoiceRepo()
	paymentRepo := document_repo.NewCryptoPaymentRepo()
	lifecycle := crypto.NewInvoiceLifecycleService(invoiceRepo, paymentRepo, nil, txm, func() time.Time { return now })

	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		results <- txm.RunInTransaction(ctx, func(txCtx context.Context) error {
			if _, lockErr := invoiceRepo.GetByIDForUpdate(txCtx, invoiceID); lockErr != nil {
				return lockErr
			}
			q := txm.GetQuerier(txCtx)
			if _, insertErr := q.Exec(txCtx, `
				INSERT INTO doc_crypto_payments(
					id,number,date,basis_type,basis_id,posted,posted_version,invoice_id,merchant_id,
					network_id,token_id,wallet_id,tx_hash,from_address,amount,event_kind,event_index,
					block_number,block_hash,confirmations,required_confs,finality_status,canonicality,
					status,detected_at,confirmed_at,receipt_id)
				VALUES($1,$2,$3,'CryptoInvoice',$4,TRUE,1,$4,$5,$6,$7,$8,$9,$10,100,
				       'contract_log',0,100,$11,20,20,'finalized','canonical','settled',$3,$3,$12);
				INSERT INTO reg_crypto_receipt_suspense_movements(
					recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
				VALUES($1,'CryptoPayment',1,$3,'expense',$12,$7,100);
				INSERT INTO reg_crypto_merchant_balance_movements(
					line_id,recorder_id,recorder_type,recorder_version,period,record_type,
					merchant_id,token_id,amount)
				VALUES($1,$1,'CryptoPayment',1,$3,'receipt',$5,$7,100)
			`, pgx.QueryExecModeSimpleProtocol, paymentID, "PAYMENT-"+suffix, now, invoiceID, merchantID, networkID,
				tokenID, walletID, txHash, "0x00000000000000000000000000000000000000AB", strings.Repeat("e", 64), receiptID); insertErr != nil {
				return insertErr
			}
			_, syncErr := lifecycle.SyncInTransaction(txCtx, invoiceID)
			return syncErr
		})
	}()
	go func() {
		defer group.Done()
		<-start
		results <- txm.RunInTransaction(ctx, func(txCtx context.Context) error {
			_, expireErr := lifecycle.ExpireInTransaction(txCtx, invoiceID)
			return expireErr
		})
	}()
	close(start)
	group.Wait()
	close(results)
	for raceErr := range results {
		require.NoError(t, raceErr)
	}

	var status crypto_invoice.InvoiceStatus
	var received, confirmed, credited int64
	var payments, merchantMovements int
	var merchantBalance int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT invoice.status,invoice.received_amount,invoice.confirmed_amount,invoice.credited_amount,
		       (SELECT COUNT(*) FROM doc_crypto_payments WHERE invoice_id=invoice.id),
		       (SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements
		        WHERE recorder_type='CryptoPayment' AND recorder_id=$2),
		       (SELECT amount FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$3 AND token_id=$4)
		FROM doc_crypto_invoices invoice WHERE invoice.id=$1
	`, invoiceID, paymentID, merchantID, tokenID).Scan(
		&status, &received, &confirmed, &credited, &payments, &merchantMovements, &merchantBalance,
	))
	require.Contains(t, []crypto_invoice.InvoiceStatus{
		crypto_invoice.InvoiceStatusExpired,
		crypto_invoice.InvoiceStatusCompleted,
	}, status)
	require.EqualValues(t, 100, received)
	require.EqualValues(t, 100, confirmed)
	require.EqualValues(t, 100, credited)
	require.Equal(t, 1, payments)
	require.Equal(t, 1, merchantMovements)
	require.EqualValues(t, 100, merchantBalance)
}

func TestInvoiceExpiryClosesTransientWalletAllocation(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	networkID, tokenID, merchantID := id.New(), id.New(), id.New()
	walletID, invoiceID, allocationID := id.New(), id.New(), id.New()
	suffix := id.Compact(invoiceID)
	now := time.Now().UTC()
	publicRef := "ci_" + strings.ReplaceAll(invoiceID.String(), "-", "")
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_blockchain_networks(
			id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,is_active)
		VALUES($1,$2,'Invoice expiry close network','tron',$3,'TRX',6,FALSE);
		INSERT INTO cat_merchants(id,code,name)
		VALUES($4,$5,'Invoice expiry close merchant');
		INSERT INTO cat_tokens(
			id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
		VALUES($6,$7,'Invoice expiry close token',$1,$8,$9,6,'TRC-20');
		INSERT INTO cat_wallets(
			id,code,name,network_id,address,address_key,tier,wallet_role,status,
			allocation_mode,is_active,chain_activation_status,leased_for_id,leased_until)
		VALUES($10,$11,'Invoice expiry close wallet',$1,$12,$12,'pool','merchant_pool','leased',
		       'transient',TRUE,'active',$13,$14);
		INSERT INTO doc_crypto_invoices(
			id,public_ref,number,date,merchant_id,token_id,wallet_id,expected_amount,
			status,expires_at,ttl_minutes,creation_source)
		VALUES($13,$15,$16,$14::timestamptz,$4,$6,$10,100,'created',
		       $14::timestamptz-interval '1 minute',5,'portal');
		INSERT INTO reg_crypto_wallet_allocations(
			id,wallet_id,invoice_id,merchant_id,token_id,network_id,leased_at)
		VALUES($17,$10,$13,$4,$6,$1,$14::timestamptz-interval '5 minutes')
	`, pgx.QueryExecModeSimpleProtocol, networkID, "IEC-NET-"+suffix, "iec-"+suffix,
		merchantID, "IEC-MERCHANT-"+suffix,
		tokenID, "IEC-TOKEN-"+suffix, "TExpiryCloseToken"+suffix, "EC"+suffix[:6],
		walletID, "IEC-WALLET-"+suffix, "TExpiryCloseWallet"+suffix,
		invoiceID, now, publicRef, "EXPIRY-CLOSE-"+suffix, allocationID)
	require.NoError(t, err)

	invoiceRepo := document_repo.NewCryptoInvoiceRepo()
	lifecycle := crypto.NewInvoiceLifecycleService(invoiceRepo, document_repo.NewCryptoPaymentRepo(), nil, txm, func() time.Time { return now })
	require.NoError(t, txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		releaseWallet, expireErr := lifecycle.ExpireInTransaction(txCtx, invoiceID)
		if expireErr != nil {
			return expireErr
		}
		require.True(t, releaseWallet)
		if lockErr := invoiceRepo.LockWalletForInvoice(txCtx, invoiceID); lockErr != nil {
			return lockErr
		}
		return invoiceRepo.ReleaseInvoiceWallet(txCtx, invoiceID, "expired")
	}))

	var invoiceStatus crypto_invoice.InvoiceStatus
	var walletStatus, closeReason string
	var walletReleased, allocationClosed bool
	var walletVersion, allocationVersion int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT invoice.status,
		       wallet.status,
		       wallet.leased_for_id IS NULL AND wallet.leased_until IS NULL,
		       allocation.closed_at IS NOT NULL AND allocation.cooldown_until IS NOT NULL,
		       allocation.close_reason,
		       wallet.version,
		       allocation.version
		FROM doc_crypto_invoices invoice
		JOIN cat_wallets wallet ON wallet.id=invoice.wallet_id
		JOIN reg_crypto_wallet_allocations allocation ON allocation.invoice_id=invoice.id
		WHERE invoice.id=$1
	`, invoiceID).Scan(&invoiceStatus, &walletStatus, &walletReleased, &allocationClosed, &closeReason, &walletVersion, &allocationVersion))
	require.Equal(t, crypto_invoice.InvoiceStatusExpired, invoiceStatus)
	require.Equal(t, "free", walletStatus)
	require.True(t, walletReleased)
	require.True(t, allocationClosed)
	require.Equal(t, "expired", closeReason)
	require.Equal(t, 2, walletVersion)
	require.Equal(t, 2, allocationVersion)
}
