//go:build integration

package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunAllFreshCryptoLifecycleSchema(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_MIGRATION_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_MIGRATION_DSN is required for integration tests")
	}

	output, err := RunAll(dsn)
	require.NoError(t, err, output)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var sweepIDColumn, observationIDColumn, withdrawalBasisIndex, withdrawalRequestBasisIndex, sweepAttemptBasisGuard bool
	var withdrawalRequestDocumentColumns int
	var walletPoolPolicy, derivationRegistry, provisioningClaimToken, zeroCostReservationGate bool
	var networkPolicy, runtimeStatus, feeReservation, nativeFunding, genericActivation, networkOperation bool
	var reservationEventsImmutable, settlementEvidenceImmutable bool
	var evmNonceState, evmNonceEventsImmutable, watcherHistory, sepoliaCatalog bool
	var merchantEvents, webhookEndpoints, webhookJobs, webhookSecretRetirements, invoiceLifecycleAmounts bool
	var externalOutflowObservations, externalOutflowObservationIdentity, externalOutflowTerminalGuards bool
	var legacyActivation bool
	require.NoError(t, db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM information_schema.columns
		WHERE table_name='reg_crypto_sweep_attempts' AND column_name='sweep_id'
	)`).Scan(&sweepIDColumn))
	require.NoError(t, db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM information_schema.columns
		WHERE table_name='doc_crypto_provider_balance_operations' AND column_name='provider_observation_id'
	)`).Scan(&observationIDColumn))
	require.NoError(t, db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM pg_indexes
		WHERE indexname='idx_crypto_withdrawals_request_basis'
	)`).Scan(&withdrawalBasisIndex))
	require.NoError(t, db.QueryRow(`SELECT COUNT(DISTINCT column_name)
		FROM information_schema.columns
		WHERE table_name='doc_withdrawal_requests'
		  AND column_name IN (
		    'id','deletion_mark','version','attributes','created_at','updated_at','created_by','updated_by',
		    'number','date','posted','posted_version','basis_type','basis_id','description'
		  )
	`).Scan(&withdrawalRequestDocumentColumns))
	require.NoError(t, db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM pg_indexes
		WHERE indexname='idx_doc_withdrawal_requests_basis'
	)`).Scan(&withdrawalRequestBasisIndex))
	require.NoError(t, db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM pg_constraint
		WHERE conname='doc_crypto_sweeps_no_internal_attempt_basis'
	)`).Scan(&sweepAttemptBasisGuard))
	require.NoError(t, db.QueryRow(`
		SELECT to_regclass('sys_crypto_wallet_pool_policies') IS NOT NULL,
		       to_regclass('sys_crypto_wallet_derivations') IS NOT NULL,
		       EXISTS (
		           SELECT 1
		           FROM information_schema.columns
		           WHERE table_name = 'sys_crypto_wallet_provisioning_jobs'
		             AND column_name = 'claim_token'
		       ),
		       EXISTS (
		           SELECT 1
		           FROM pg_indexes
		           WHERE indexname = 'idx_crypto_activation_budget_reservations'
		             AND indexdef LIKE '%activation_cost_reserved_at IS NOT NULL%'
		       )
	`).Scan(
		&walletPoolPolicy,
		&derivationRegistry,
		&provisioningClaimToken,
		&zeroCostReservationGate,
	))
	require.NoError(t, db.QueryRow(`
		SELECT to_regclass('sys_crypto_network_policies') IS NOT NULL,
		       to_regclass('sys_crypto_network_runtime_status') IS NOT NULL,
		       to_regclass('reg_crypto_fee_reservations') IS NOT NULL,
		       to_regclass('doc_crypto_native_fundings') IS NOT NULL,
		       to_regclass('reg_crypto_account_activations') IS NOT NULL,
		       to_regclass('reg_tron_account_activations') IS NOT NULL,
		       to_regclass('doc_crypto_network_operations') IS NOT NULL
	`).Scan(&networkPolicy, &runtimeStatus, &feeReservation, &nativeFunding, &genericActivation, &legacyActivation, &networkOperation))
	require.NoError(t, db.QueryRow(`
		SELECT EXISTS (
		         SELECT 1 FROM pg_trigger
		         WHERE tgname='trg_crypto_fee_reservation_events_immutable' AND NOT tgisinternal
		       ),
		       EXISTS (
		         SELECT 1 FROM pg_trigger
		         WHERE tgname='trg_crypto_chain_settlements_immutable' AND NOT tgisinternal
		       )
	`).Scan(&reservationEventsImmutable, &settlementEvidenceImmutable))
	require.NoError(t, db.QueryRow(`
		SELECT to_regclass('reg_evm_nonce_cursors') IS NOT NULL
		       AND to_regclass('reg_evm_nonce_leases') IS NOT NULL,
		       EXISTS (
		         SELECT 1 FROM pg_trigger
		         WHERE tgname='trg_evm_nonce_lease_events_immutable' AND NOT tgisinternal
		       ),
		       to_regclass('sys_chain_watcher_block_history') IS NOT NULL,
		       EXISTS (
		         SELECT 1 FROM cat_blockchain_networks n
		         JOIN cat_tokens eth ON eth.network_id=n.id AND eth.code='ETH-SEPOLIA'
		         JOIN cat_tokens pyusd ON pyusd.network_id=n.id AND pyusd.code='PYUSD-SEPOLIA'
		         WHERE n.code='ETHEREUM-SEPOLIA' AND n.chain_family='evm'
		           AND n.network_identifier='11155111' AND n.confirmations_needed=12
		           AND n.finality_mode='confirmations_or_finalized' AND n.is_active=FALSE
		           AND eth.decimal_places=18 AND eth.token_standard='native'
		           AND pyusd.decimal_places=6 AND pyusd.token_standard='ERC-20'
		           AND pyusd.contract_address='0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB9'
		       )
	`).Scan(&evmNonceState, &evmNonceEventsImmutable, &watcherHistory, &sepoliaCatalog))
	require.NoError(t, db.QueryRow(`
		SELECT to_regclass('sys_merchant_events') IS NOT NULL,
		       to_regclass('cat_merchant_webhook_endpoints') IS NOT NULL,
		       to_regclass('sys_webhook_delivery_jobs') IS NOT NULL,
		       to_regclass('sys_webhook_secret_retirements') IS NOT NULL,
		       EXISTS (
		         SELECT 1 FROM information_schema.columns
		         WHERE table_name='doc_crypto_invoices' AND column_name='confirmed_amount'
		       ) AND EXISTS (
		         SELECT 1 FROM information_schema.columns
		         WHERE table_name='doc_crypto_invoices' AND column_name='credited_amount'
		       ) AND EXISTS (
		         SELECT 1 FROM information_schema.columns
		         WHERE table_name='doc_crypto_invoices' AND column_name='view_count'
		       ) AND EXISTS (
		         SELECT 1 FROM information_schema.columns
		         WHERE table_name='doc_crypto_invoices' AND column_name='first_viewed_at'
		       ) AND EXISTS (
		         SELECT 1 FROM information_schema.columns
		         WHERE table_name='doc_crypto_invoices' AND column_name='last_viewed_at'
		       )
	`).Scan(&merchantEvents, &webhookEndpoints, &webhookJobs, &webhookSecretRetirements, &invoiceLifecycleAmounts))
	require.NoError(t, db.QueryRow(`
		SELECT to_regclass('reg_crypto_external_outflow_observations') IS NOT NULL,
		       EXISTS (
		         SELECT 1 FROM pg_constraint
		         WHERE conname='fk_crypto_external_outflow_observation_identity'
		       ),
		       (SELECT COUNT(*)=2 FROM pg_trigger
		        WHERE tgname IN ('trg_external_outflow_incident_state','trg_external_outflow_reconciliation_state')
		          AND NOT tgisinternal)
	`).Scan(&externalOutflowObservations, &externalOutflowObservationIdentity, &externalOutflowTerminalGuards))
	require.True(t, sweepIDColumn)
	require.True(t, observationIDColumn)
	require.True(t, withdrawalBasisIndex)
	require.Equal(t, 15, withdrawalRequestDocumentColumns)
	require.True(t, withdrawalRequestBasisIndex)
	require.True(t, sweepAttemptBasisGuard)
	require.True(t, walletPoolPolicy)
	require.True(t, derivationRegistry)
	require.True(t, provisioningClaimToken)
	require.True(t, zeroCostReservationGate)
	require.True(t, networkPolicy)
	require.True(t, runtimeStatus)
	require.True(t, feeReservation)
	require.True(t, nativeFunding)
	require.True(t, genericActivation)
	require.False(t, legacyActivation)
	require.True(t, networkOperation)
	require.True(t, reservationEventsImmutable)
	require.True(t, settlementEvidenceImmutable)
	require.True(t, evmNonceState)
	require.True(t, evmNonceEventsImmutable)
	require.True(t, watcherHistory)
	require.True(t, sepoliaCatalog)
	require.True(t, merchantEvents)
	require.True(t, webhookEndpoints)
	require.True(t, webhookJobs)
	require.True(t, webhookSecretRetirements)
	require.True(t, invoiceLifecycleAmounts)
	require.True(t, externalOutflowObservations)
	require.True(t, externalOutflowObservationIdentity)
	require.True(t, externalOutflowTerminalGuards)

	provider, err := newProvider(coreMigrationsDir, db)
	require.NoError(t, err)
	// Migration 60 protects immutable financial history and is forward-only.
	// A rejected rollback must preserve both the schema and goose history.
	// Empty additive AML migrations can roll back before reaching the
	// immutable financial boundary at version 60.
	_, err = provider.DownTo(context.Background(), 59)
	require.ErrorContains(t, err, "forward-only")
	var version int64
	require.NoError(t, db.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version))
	require.EqualValues(t, 60, version)
	require.NoError(t, db.QueryRow(`SELECT to_regclass('reg_crypto_transfer_verifications') IS NOT NULL`).Scan(&watcherHistory))
	require.True(t, watcherHistory)
}
