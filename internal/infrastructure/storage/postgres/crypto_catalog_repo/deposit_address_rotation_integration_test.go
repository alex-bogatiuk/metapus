//go:build integration

package crypto_catalog_repo_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/migration"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
)

type rotationFixture struct {
	ctx                                         context.Context
	pool                                        *pgxpool.Pool
	repo                                        *crypto_catalog_repo.WalletRepo
	service                                     *cryptoapp.DepositAddressRotationService
	merchant, network, token, old, spare, actor id.ID
}

func newRotationFixture(t *testing.T) rotationFixture {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_ADMIN_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_ADMIN_DSN is required for isolated rotation tests")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	name := "rotation_test_" + id.Compact(id.New())
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	address, err := url.Parse(dsn)
	require.NoError(t, err)
	address.Path = "/" + name
	output, err := migration.RunAll(address.String())
	require.NoError(t, err, output)
	pool, err := postgres.OpenPool(t.Context(), address.String())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	f := rotationFixture{pool: pool, repo: crypto_catalog_repo.NewWalletRepo(), merchant: id.New(), network: id.New(), token: id.New(), old: id.New(), spare: id.New(), actor: id.New()}
	f.ctx = tenant.WithTxManager(t.Context(), postgres.NewTxManagerFromRawPool(pool))
	f.service = cryptoapp.NewDepositAddressRotationService(f.repo, crypto_repo.NewLifecycleWebhookOutbox())
	_, err = pool.Exec(f.ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,'ROTATION','Rotation test')`, f.merchant)
	require.NoError(t, err)
	_, err = pool.Exec(f.ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
 VALUES($1,'ROTATION','Rotation network','tron','rotation-test','TRX',6)`, f.network)
	require.NoError(t, err)
	_, err = pool.Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
 VALUES($1,'ROT-USDT','USDT',$2,'rotation-contract','USDT',6,'trc20')`, f.token, f.network)
	require.NoError(t, err)
	_, err = pool.Exec(f.ctx, `UPDATE sys_crypto_wallet_pool_policies SET enabled=TRUE,needs_review=FALSE,min_global_ready_reserve=0,
 max_persistent_addresses_per_merchant=10,max_lease_starts_per_minute_per_merchant=100 WHERE network_id=$1`, f.network)
	require.NoError(t, err)
	_, err = pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,per_transaction_max_fee_minor=1,daily_network_fee_budget_minor=1000,fee_funding_mode='source_only' WHERE network_id=$1`, f.network)
	require.NoError(t, err)
	for _, walletID := range []id.ID{f.old, f.spare} {
		_, err = pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,merchant_id,custody_merchant_id,address,vault_key_id,derivation_path,tier,status,allocation_mode,chain_activation_status)
 VALUES($1,$2,'Rotation wallet',$3,$4,$4,$5,'rotation-test-key',$2,'pool','free','transient','active')`, walletID, walletID.String(), f.network, f.merchant, "T"+id.Compact(walletID))
		require.NoError(t, err)
	}
	_, err = pool.Exec(f.ctx, `UPDATE cat_wallets SET status='assigned',allocation_mode='persistent',customer_id='customer-1',deposit_assigned_at=NOW()-INTERVAL '2 days' WHERE id=$1`, f.old)
	require.NoError(t, err)
	return f
}

func (f rotationFixture) command() wallet.DepositRotationCommand {
	return wallet.DepositRotationCommand{MerchantID: f.merchant, WalletID: f.old, ActorID: f.actor, Reason: "customer_request", IdempotencyKey: "rotation-request"}
}

func (f rotationFixture) use(t *testing.T, walletID id.ID) {
	t.Helper()
	invoiceID, paymentID := id.New(), id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_invoices(id,public_ref,number,date,merchant_id,token_id,wallet_id,expected_amount,expires_at)
 VALUES($1,$2,$3,NOW(),$4,$5,$6,5000000,NOW()+INTERVAL '1 hour')`, invoiceID, "ci_"+id.Compact(invoiceID), invoiceID.String(), f.merchant, f.token, walletID)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO doc_crypto_payments(id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,event_kind,amount,status,canonicality,confirmed_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,'contract_log',5000000,'confirmed','canonical',NOW())`, paymentID, invoiceID, f.merchant, f.network, f.token, walletID, paymentID.String())
	require.NoError(t, err)
}

func (f rotationFixture) assertUnchanged(t *testing.T) {
	t.Helper()
	var retired *time.Time
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT deposit_retired_at FROM cat_wallets WHERE id=$1`, f.old).Scan(&retired))
	require.Nil(t, retired)
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_deposit_address_rotations`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM sys_outbox WHERE event_type='deposit_address.rotated'`).Scan(&count))
	require.Zero(t, count)
}

func TestDepositRotationAtomicReplayAndPermanentAttribution(t *testing.T) {
	f := newRotationFixture(t)
	f.use(t, f.old)
	result, err := f.service.Rotate(f.ctx, f.command())
	require.NoError(t, err)
	require.Equal(t, f.spare, result.NewWalletID)
	replay, err := f.service.Rotate(f.ctx, f.command())
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, result.ID, replay.ID)
	require.Equal(t, result.CreatedAt, replay.CreatedAt)
	current, err := f.repo.FindPersistentByCustomerID(f.ctx, f.merchant, f.network, "customer-1")
	require.NoError(t, err)
	require.Equal(t, f.spare, current.ID)
	old, err := f.repo.GetByID(f.ctx, f.old)
	require.NoError(t, err)
	require.NotNil(t, old.DepositRetiredAt)
	require.True(t, old.IsActive)
	require.Equal(t, "customer-1", old.CustomerID)
	require.Equal(t, f.merchant, *old.MerchantID)
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM sys_outbox WHERE event_type='deposit_address.rotated'`).Scan(&count))
	require.Equal(t, 1, count)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET deposit_retired_at=NULL WHERE id=$1`, f.old)
	require.ErrorContains(t, err, "immutable")
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET status='sweep_pending' WHERE id=$1`, f.old)
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `DELETE FROM cat_wallets WHERE id=$1`, f.old)
	require.Error(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_deposit_address_rotations SET reason='aml_high_risk' WHERE id=$1`, result.ID)
	require.ErrorContains(t, err, "immutable")
	ctx := merchant.WithPortalScope(f.ctx, merchant.PortalScope{MerchantIDs: []id.ID{f.merchant}})
	items, total, quotas, err := portal_repo.NewDashboardRepo().ListDepositAddresses(ctx, f.merchant, portal_repo.DepositAddressFilter{}, 20, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, items, 1)
	require.Equal(t, f.spare.String(), items[0].ID)
	require.False(t, items[0].CanRotate)
	require.Len(t, quotas, 1)
	require.Equal(t, 1, quotas[0].Used)
	conflict := f.command()
	conflict.Reason = "aml_high_risk"
	_, err = f.service.Rotate(f.ctx, conflict)
	conflictErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, apperror.CodeConflict, conflictErr.Code)
}

type rotationFailPublisher struct{}

func (rotationFailPublisher) PublishLifecycleWebhook(context.Context, crypto.LifecycleWebhookEvent) error {
	return errors.New("synthetic outbox failure")
}

func TestDepositRotationFailuresDoNotConsumeAddresses(t *testing.T) {
	for _, scenario := range []string{"unused", "exhausted", "used_spare", "webhook_failure", "wrong_merchant", "pending_payment", "sweeping"} {
		t.Run(scenario, func(t *testing.T) {
			f := newRotationFixture(t)
			if scenario != "unused" {
				f.use(t, f.old)
			}
			command := f.command()
			service := f.service
			switch scenario {
			case "exhausted":
				_, err := f.pool.Exec(f.ctx, `UPDATE cat_wallets SET chain_activation_status='held' WHERE id=$1`, f.spare)
				require.NoError(t, err)
			case "used_spare":
				f.use(t, f.spare)
			case "webhook_failure":
				service = cryptoapp.NewDepositAddressRotationService(f.repo, rotationFailPublisher{})
			case "wrong_merchant":
				command.MerchantID = id.New()
			case "pending_payment":
				_, err := f.pool.Exec(f.ctx, `UPDATE doc_crypto_payments SET status='confirming' WHERE wallet_id=$1`, f.old)
				require.NoError(t, err)
			case "sweeping":
				_, err := f.pool.Exec(f.ctx, `UPDATE cat_wallets SET status='sweeping' WHERE id=$1`, f.old)
				require.NoError(t, err)
			}
			_, err := service.Rotate(f.ctx, command)
			require.Error(t, err)
			f.assertUnchanged(t)
		})
	}
}

func TestDepositRotationConcurrentRequestsAndCooldown(t *testing.T) {
	f := newRotationFixture(t)
	f.use(t, f.old)
	var wg sync.WaitGroup
	results := make(chan *wallet.DepositRotation, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { result, err := f.service.Rotate(f.ctx, f.command()); results <- result; errs <- err })
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var first id.ID
	for result := range results {
		if id.IsNil(first) {
			first = result.ID
		}
		require.Equal(t, first, result.ID)
	}
	f.use(t, f.spare)
	command := f.command()
	command.WalletID = f.spare
	command.IdempotencyKey = "second-rotation"
	_, err := f.service.Rotate(f.ctx, command)
	require.Error(t, err)
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, "rotation_cooldown", appErr.Details["reason"])
}

func TestRetiredDepositIntakeNeverCreatesPaymentAndWaitsForReceipt(t *testing.T) {
	f := newRotationFixture(t)
	f.use(t, f.old)
	rotation, err := f.service.Rotate(f.ctx, f.command())
	require.NoError(t, err)
	inbox := crypto_repo.NewChainEventInboxRepo()
	txm := postgres.MustGetTxManager(f.ctx)
	processor := crypto.NewEventProcessor(crypto.EventProcessorConfig{
		WalletSvc: wallet.NewService(f.repo, nil), PaymentRepo: crypto_document_repo.NewCryptoPaymentRepo(), InvoiceSvc: crypto_document_repo.NewCryptoInvoiceRepo(),
		TokenResolver: crypto_catalog_repo.NewTokenRepo(), TxManager: txm, Inbox: inbox,
	})
	event := crypto.BlockchainEvent{NetworkID: f.network, TxHash: "late-retired-transfer", ToAddress: rotation.OldAddress, FromAddress: "external-payer",
		TokenContract: "rotation-contract", Amount: types.MustCryptoAmount("7000000"), EventKind: crypto.EventKindContractLog, EventType: crypto.EventTypeTransfer,
		Direction: crypto.TransferDirectionIncoming, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalityProvisional, RequiredConfs: 2, Confirmations: 1, Timestamp: time.Now().UTC()}
	for range 2 {
		require.NoError(t, processor.ProcessEvent(f.ctx, event))
	}
	var reason, status string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT match_reason,match_status FROM reg_crypto_inbound_chain_events WHERE tx_hash='late-retired-transfer'`).Scan(&reason, &status))
	require.Equal(t, crypto.MatchReasonRetiredDepositAddress, reason)
	require.Equal(t, string(crypto.InboxNeedsReview), status)
	// The transfer waits in the journal of pending receipts until it is final and proven.
	journal, err := cryptoapp.NewReceiptJournal(crypto_repo.NewUnidentifiedReceiptRepo(), crypto_repo.NewWalletFundingJournalRepo()).List(f.ctx, cryptoapp.ReceiptJournalPending, false, 50, 0)
	require.NoError(t, err)
	require.Len(t, journal.Items, 1)
	require.Equal(t, crypto.MatchReasonRetiredDepositAddress, journal.Items[0].Reason)
	event.EventType = crypto.EventTypeConfirmation
	event.Confirmations = 2
	event.Finality = crypto.FinalitySolidified
	require.NoError(t, processor.ProcessEvent(f.ctx, event))
	event.EventType = crypto.EventTypeTransfer
	require.NoError(t, processor.ProcessEvent(f.ctx, event))
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM doc_crypto_payments WHERE tx_hash='late-retired-transfer'`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements`).Scan(&count))
	require.Zero(t, count)
}

func (f rotationFixture) customerWallet(t *testing.T, networkID id.ID, customer string) id.ID {
	t.Helper()
	walletID := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,merchant_id,custody_merchant_id,address,vault_key_id,derivation_path,status,allocation_mode,customer_id,chain_activation_status)
 VALUES($1,$2,'Customer address',$3,$4,$4,$5,'rotation-key',$2,'assigned','persistent',$6,'active')`, walletID, walletID.String(), networkID, f.merchant, "T"+id.Compact(walletID), customer)
	require.NoError(t, err)
	return walletID
}

func TestDepositRotationCooldownCrossesNetworks(t *testing.T) {
	f := newRotationFixture(t)
	f.use(t, f.old)
	_, err := f.service.Rotate(f.ctx, f.command())
	require.NoError(t, err)
	other := f
	other.network = id.New()
	other.token = id.New()
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
 VALUES($1,'SECOND','Second network','tron','rotation-second','TRX',6)`, other.network)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
 VALUES($1,'SECOND-USDT','USDT',$2,'second-contract','USDT',6,'trc20')`, other.token, other.network)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_wallet_pool_policies SET enabled=TRUE,needs_review=FALSE WHERE network_id=$1`, other.network)
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_network_policies SET enabled=TRUE,per_transaction_max_fee_minor=1,daily_network_fee_budget_minor=1000,fee_funding_mode='source_only' WHERE network_id=$1`, other.network)
	require.NoError(t, err)
	other.old = f.customerWallet(t, other.network, "customer-1")
	other.use(t, other.old)
	command := other.command()
	command.IdempotencyKey = "different-network"
	_, err = f.service.Rotate(f.ctx, command)
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, "rotation_cooldown", appErr.Details["reason"])
}

func TestDepositRotationMerchantDailyQuota(t *testing.T) {
	f := newRotationFixture(t)
	f.use(t, f.old)
	_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_wallet_pool_policies SET max_persistent_addresses_per_merchant=1 WHERE network_id=$1`, f.network)
	require.NoError(t, err)
	_, err = f.service.Rotate(f.ctx, f.command())
	require.NoError(t, err)
	other := f.customerWallet(t, f.network, "customer-2")
	f.use(t, other)
	command := f.command()
	command.WalletID = other
	command.IdempotencyKey = "different-customer"
	_, err = f.service.Rotate(f.ctx, command)
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, "merchant_rotation_limit", appErr.Details["reason"])
}

func TestDepositRotationCompetingDifferentKeysConsumeOneAddress(t *testing.T) {
	f := newRotationFixture(t)
	f.use(t, f.old)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, key := range []string{"first", "second"} {
		wg.Go(func() {
			command := f.command()
			command.IdempotencyKey = key
			_, err := f.service.Rotate(f.ctx, command)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else {
			appErr, ok := apperror.AsAppError(err)
			require.True(t, ok)
			require.Equal(t, apperror.CodeConflict, appErr.Code)
		}
	}
	require.Equal(t, 1, successes)
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_deposit_address_rotations`).Scan(&count))
	require.Equal(t, 1, count)
}
