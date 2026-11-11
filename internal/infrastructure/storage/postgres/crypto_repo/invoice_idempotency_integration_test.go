//go:build integration

package crypto_repo

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/application/command"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	corenumerator "metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/tenantdb"
)

func TestTwentyConcurrentInvoiceCommandsCreateOneAggregateAllocationAndEvent(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenantdb.WithPool(tenant.WithTxManager(ctx, txm), pool)

	networkID, tokenID, merchantID, walletID := id.New(), id.New(), id.New(), id.New()
	suffix := id.Compact(networkID)
	now := time.Now().UTC()
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_blockchain_networks(
			id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,
			confirmations_needed,is_active)
		VALUES($1,$2,'Invoice idempotency network','tron',$3,'TRX',6,20,TRUE);
		UPDATE sys_crypto_network_policies
		SET enabled=TRUE,per_transaction_max_fee_minor=1,daily_network_fee_budget_minor=100
		WHERE network_id=$1;
		UPDATE sys_crypto_wallet_pool_policies
		SET enabled=TRUE,low_watermark=1,target_ready=2,max_total_wallets=2,max_provision_batch=1,
		    max_active_invoice_leases_per_merchant=20,max_lease_starts_per_minute_per_merchant=100,
		    min_global_ready_reserve=0,needs_review=FALSE,derivation_namespace=$4
		WHERE network_id=$1;
		INSERT INTO cat_merchants(id,code,name)
		VALUES($5,$6,'Invoice idempotency merchant');
		INSERT INTO cat_tokens(
			id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
		VALUES($7,$8,'Invoice idempotency token',$1,$9,$10,6,'TRC-20');
		INSERT INTO cat_wallets(
			id,code,name,network_id,address,address_key,derivation_path,vault_key_id,
			tier,wallet_role,status,allocation_mode,is_active,chain_activation_status)
		VALUES($11,$12,'Invoice idempotency wallet',$1,$13,$13,$14,$15,
		       'pool','merchant_pool','free','transient',TRUE,'active')
	`, pgx.QueryExecModeSimpleProtocol, networkID, "III-NET-"+suffix, "iii-"+suffix,
		"m/44'/195'/9000'/0", merchantID, "III-MERCHANT-"+suffix,
		tokenID, "III-TOKEN-"+suffix, "TInvoiceToken"+suffix, "II"+suffix[:6],
		walletID, "III-WALLET-"+suffix, "TInvoiceWallet"+suffix,
		"m/44'/195'/9000'/0/0", "vault/invoice-idempotency/"+suffix)
	require.NoError(t, err)

	invoiceRepo := document_repo.NewCryptoInvoiceRepo()
	invoiceService := crypto_invoice.NewService(invoiceRepo, nil, numerator.New(), nil)
	walletService := wallet.NewService(catalog_repo.NewWalletRepo(), corenumerator.Noop())
	allocationRepo := NewWalletAllocationRepo()
	lifecycle := NewLifecycleWebhookOutbox()
	invoiceService.Hooks().OnBeforeCreate(func(hookCtx context.Context, invoice *crypto_invoice.CryptoInvoice) error {
		leased, leaseErr := walletService.LeaseForInvoice(hookCtx, invoice.MerchantID, invoice.ID, networkID, invoice.TokenID, invoice.ExpiresAt)
		if leaseErr != nil {
			return leaseErr
		}
		invoice.WalletID = &leased.ID
		return allocationRepo.Open(hookCtx, crypto.WalletAllocation{
			WalletID: leased.ID, InvoiceID: invoice.ID, MerchantID: invoice.MerchantID,
			TokenID: invoice.TokenID, NetworkID: networkID,
		})
	})
	invoiceService.OnCreatedInTransaction(func(hookCtx context.Context, invoice *crypto_invoice.CryptoInvoice) error {
		invoiceID := invoice.ID
		return lifecycle.PublishLifecycleWebhook(hookCtx, crypto.LifecycleWebhookEvent{
			Event: crypto.WebhookInvoiceCreated, AggregateID: invoice.ID, MerchantID: invoice.MerchantID,
			InvoiceID: &invoiceID, ResourceVersion: invoice.Version, OccurredAt: now,
			Data: crypto.InvoiceWebhookData(invoice, nil),
		})
	})
	invoiceApp, err := cryptoapp.NewMerchantInvoiceService(cryptoapp.MerchantInvoiceServiceConfig{
		InvoiceService: invoiceService, InvoiceRepo: invoiceRepo,
		IsUniqueViolation: postgres.IsUniqueViolation, Now: func() time.Time { return now },
	})
	require.NoError(t, err)

	input := cryptoapp.CreateMerchantInvoiceInput{
		MerchantID: merchantID, TokenID: tokenID, Amount: types.MustCryptoAmount("42000000"),
		CreatedByID: id.New(), CreationSource: crypto_invoice.CreationSourcePortal,
		ExpiresAt: now.Add(time.Hour), TTLMinutes: 60, OrderID: "order-" + suffix,
		ExternalID: "external-" + suffix, Description: "one user operation",
		Metadata: map[string]string{"test": "concurrent"}, RequiredConfirmations: 20,
		FinalityMode: "confirmations",
	}
	fingerprint, err := command.Fingerprint(input)
	require.NoError(t, err)
	metadata := command.Metadata{
		ScopeType: "merchant", ScopeID: merchantID.String(), Operation: "POST /merchant/v1/invoices",
		IdempotencyKey: "invoice-" + suffix, RequestFingerprint: fingerprint,
	}
	executor := command.NewExecutor(txm, postgres.NewCommandRepository())
	type result struct {
		execution command.Execution
		err       error
	}
	results := make(chan result, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			execution, executeErr := executor.Execute(ctx, metadata, func(commandCtx context.Context) (command.Result, error) {
				created, createErr := invoiceApp.CreateMerchantInvoice(commandCtx, input)
				if createErr != nil {
					return command.Result{}, createErr
				}
				payload, marshalErr := json.Marshal(map[string]any{"id": created.Invoice.ID.String(), "version": created.Invoice.Version})
				return command.Result{
					Outcome: "created", ResourceType: "CryptoInvoice", ResourceID: created.Invoice.ID.String(),
					ResourceVersion: int64(created.Invoice.Version), Payload: payload,
				}, marshalErr
			})
			results <- result{execution: execution, err: executeErr}
		}()
	}
	wg.Wait()
	close(results)

	var resourceID string
	var originals, replays int
	for item := range results {
		require.NoError(t, item.err)
		if resourceID == "" {
			resourceID = item.execution.Result.ResourceID
		}
		require.Equal(t, resourceID, item.execution.Result.ResourceID)
		if item.execution.Replayed {
			replays++
		} else {
			originals++
		}
	}
	require.Equal(t, 1, originals)
	require.Equal(t, 19, replays)

	invoiceID, err := id.Parse(resourceID)
	require.NoError(t, err)
	var invoices, allocations, merchantEvents, outboxEvents, leasedWallets int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM doc_crypto_invoices WHERE merchant_id=$1 AND order_id=$2),
			(SELECT COUNT(*) FROM reg_crypto_wallet_allocations WHERE invoice_id=$3 AND closed_at IS NULL),
			(SELECT COUNT(*) FROM sys_merchant_events WHERE aggregate_id=$3 AND event_type='invoice.created'),
			(SELECT COUNT(*) FROM sys_outbox WHERE aggregate_id=$3 AND event_type='invoice.created'),
			(SELECT COUNT(*) FROM cat_wallets WHERE id=$4 AND status='leased' AND leased_for_id=$3)
	`, merchantID, input.OrderID, invoiceID, walletID).Scan(
		&invoices, &allocations, &merchantEvents, &outboxEvents, &leasedWallets,
	))
	require.Equal(t, 1, invoices, fmt.Sprintf("invoice aggregate count for %s", invoiceID))
	require.Equal(t, 1, allocations)
	require.Equal(t, 1, merchantEvents)
	require.Equal(t, 1, outboxEvents)
	require.Equal(t, 1, leasedWallets)

	secondWalletID := id.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_wallets(
			id,code,name,network_id,address,address_key,derivation_path,vault_key_id,
			tier,wallet_role,status,allocation_mode,is_active,chain_activation_status)
		VALUES($1,$2,'Single free wallet race',$3,$4,$4,$5,$6,
		       'pool','merchant_pool','free','transient',TRUE,'active')
	`, secondWalletID, "II-RACE-WALLET-"+suffix, networkID, "TInvoiceRaceWallet"+suffix,
		"m/44'/195'/9000'/0/1", "vault/invoice-race/"+suffix)
	require.NoError(t, err)

	firstDistinct, secondDistinct := input, input
	firstDistinct.OrderID, firstDistinct.ExternalID = "race-order-a-"+suffix, "race-external-a-"+suffix
	secondDistinct.OrderID, secondDistinct.ExternalID = "race-order-b-"+suffix, "race-external-b-"+suffix
	distinctInputs := []cryptoapp.CreateMerchantInvoiceInput{firstDistinct, secondDistinct}
	distinctErrors := make(chan error, 2)
	for _, distinctInput := range distinctInputs {
		wg.Add(1)
		go func(distinctInput cryptoapp.CreateMerchantInvoiceInput) {
			defer wg.Done()
			_, createErr := invoiceApp.CreateMerchantInvoice(ctx, distinctInput)
			distinctErrors <- createErr
		}(distinctInput)
	}
	wg.Wait()
	close(distinctErrors)
	var distinctSuccesses, capacityFailures int
	for createErr := range distinctErrors {
		if createErr == nil {
			distinctSuccesses++
			continue
		}
		appErr, ok := apperror.AsAppError(createErr)
		if ok && appErr.Code == apperror.CodeWalletPoolExhausted {
			capacityFailures++
			continue
		}
		t.Fatalf("unexpected distinct invoice race error: %v", createErr)
	}
	require.Equal(t, 1, distinctSuccesses)
	require.Equal(t, 1, capacityFailures)

	var distinctInvoices, secondWalletAllocations int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM doc_crypto_invoices WHERE merchant_id=$1 AND order_id IN ($2,$3)),
			(SELECT COUNT(*) FROM reg_crypto_wallet_allocations WHERE wallet_id=$4 AND closed_at IS NULL)
	`, merchantID, firstDistinct.OrderID, secondDistinct.OrderID, secondWalletID).Scan(
		&distinctInvoices, &secondWalletAllocations,
	))
	require.Equal(t, 1, distinctInvoices)
	require.Equal(t, 1, secondWalletAllocations)
}
