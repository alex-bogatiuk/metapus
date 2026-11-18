package migration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dbassets "metapus/db"
	"metapus/internal/core/types"
)

func TestCryptoInvoiceIdempotencyIndexesAreInBaseMigration(t *testing.T) {
	sql := readMigrationSQL(t, "00006_crypto_core.sql")

	require.Contains(t, sql, "public_ref    TEXT        NOT NULL")
	require.Contains(t, sql, "CONSTRAINT uq_crypto_invoice_public_ref UNIQUE (public_ref)")
	require.NotContains(t, sql, "public_ref    TEXT        NOT NULL DEFAULT")
	require.Contains(t, sql, "CREATE UNIQUE INDEX idx_crypto_invoices_merchant_order_id")
	require.Contains(t, sql, "ON doc_crypto_invoices (merchant_id, order_id)")
	require.Contains(t, sql, "WHERE order_id <> '' AND _deleted_at IS NULL")
	require.Contains(t, sql, "CREATE UNIQUE INDEX idx_crypto_invoices_merchant_external_id")
	require.Contains(t, sql, "ON doc_crypto_invoices (merchant_id, external_id)")
	require.Contains(t, sql, "WHERE external_id <> '' AND _deleted_at IS NULL")
}

func TestEachMigrationDirectionIsOneGooseStatement(t *testing.T) {
	files := tenantMigrationFiles(t)
	require.NotEmpty(t, files)
	for _, file := range files {
		require.NoError(t, dbassets.CheckMigrationFormat(readMigrationSQL(t, filepath.Base(file))), file)
	}
	core := readMigrationSQL(t, "00001_core.sql")
	require.Contains(t, core, "CREATE OR REPLACE FUNCTION fn_sys_idempotency_guard()")
	require.Contains(t, core, "CREATE TRIGGER trg_sys_idempotency_guard")
}

func TestBlockchainExplorerTemplateContractIsInBaseMigrationAndSeed(t *testing.T) {
	migrationSQL := readMigrationSQL(t, "00006_crypto_core.sql")
	seed := readSQL(t, "cmd", "seed", "main.go")

	require.Contains(t, migrationSQL, "chk_blockchain_network_explorer_url")
	require.Contains(t, migrationSQL, "{txHash}")
	require.Contains(t, migrationSQL, "^https://")
	require.Contains(t, seed, `https://nile.tronscan.org/#/transaction/{txHash}`)
}

func TestPublicCheckoutMetaSchemaExists(t *testing.T) {
	sql := readSQL(t, "db", "meta", "00001_tenants.sql")

	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS public_checkout_refs")
	require.Regexp(t, `(?m)^\s*ref_hash\s+CHAR\(64\)\s+PRIMARY KEY`, sql)
	require.Contains(t, sql, "CONSTRAINT uq_public_checkout_refs_resource UNIQUE (tenant_id, resource_type, resource_id)")
	require.Contains(t, sql, "CHECK (status IN ('reserved', 'active', 'disabled'))")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS checkout_domains")
	require.Regexp(t, `(?m)^\s*host\s+VARCHAR\(255\)\s+PRIMARY KEY`, sql)
}

func TestPaymentLinksUsePublicRefInsteadOfShortCode(t *testing.T) {
	sql := readMigrationSQL(t, "00006_crypto_core.sql")

	require.Regexp(t, `(?m)^\s*public_ref\s+TEXT\s+NOT NULL`, sql)
	require.Contains(t, sql, "CONSTRAINT uq_payment_link_public_ref UNIQUE (public_ref)")
	require.Contains(t, sql, "CHECK (public_ref ~ '^pl_[a-f0-9]{32}$')")
	require.NotContains(t, sql, "short_code")
	require.NotContains(t, sql, "shortCode")
}

func TestWithdrawalRequestMigrationUsesGoGeneratedDocumentID(t *testing.T) {
	sql := readMigrationSQL(t, "00007_crypto_outbound.sql")
	table := tableSQL(t, sql, "doc_withdrawal_requests")

	require.Regexp(t, `(?m)^\s*id\s+UUID\s+PRIMARY KEY,`, table)
	require.NotContains(t, table, "DEFAULT gen_random_uuid_v7()")
	require.Contains(t, sql, "NEW._deleted_at := NOW();")
	require.Contains(t, sql, "NEW._deleted_at := NULL;")
	require.Contains(t, sql, "NEW._txid := txid_current();")
	require.Contains(t, sql, "NEW.updated_at := NOW();")
	require.Contains(t, sql, "'partial_failed','held'")
}

func TestLegacyTRONResourceOrderSchemaIsAbsent(t *testing.T) {
	migrationFiles := []string{"00007_crypto_outbound.sql"}
	for _, file := range migrationFiles {
		sql := strings.ToLower(readMigrationSQL(t, file))
		require.NotContains(t, sql, "reg_tron_resource_orders", file)
		require.NotContains(t, sql, "collect_manual", file)
		require.NotContains(t, sql, "legacy_recorded", file)
	}
}

func TestNativeWalletFundingMigrationIsAuditedAndIdempotent(t *testing.T) {
	sql := readMigrationSQL(t, "00006_crypto_core.sql")

	require.Contains(t, sql, "CREATE TABLE reg_crypto_wallet_fundings")
	require.Contains(t, sql, "crypto wallet funding receipts are immutable")
	require.Contains(t, readMigrationSQL(t, "00009_crypto_custody.sql"), "'CryptoWalletFunding'")
	require.NotContains(t, sql, "'TRONAccountActivation'")
	require.NotContains(t, sql, "reg_tron_account_activations")
}

func TestCryptoNetworkOperationOwnsActivationAccountingFacts(t *testing.T) {
	sql := readMigrationSQL(t, "00007_crypto_outbound.sql")
	platformCostSQL := readMigrationSQL(t, "00007_crypto_outbound.sql")

	require.Contains(t, sql, "CREATE TABLE doc_crypto_network_operations")
	require.Contains(t, sql, "operation_type IN ('wallet_activation')")
	require.Contains(t, sql, "network_operation_id")
	require.Contains(t, sql, "UNIQUE REFERENCES doc_crypto_network_operations(id)")
	require.Contains(t, sql, "idx_crypto_network_operations_chain_tx")
	require.Contains(t, sql, "idx_crypto_network_operations_basis")
	require.Contains(t, sql, "activation_cost_reserved_minor NUMERIC(78,0)")
	require.Contains(t, sql, "activation_claim_token")
	require.Contains(t, sql, "cost_status")
	require.Contains(t, sql, "idx_crypto_account_activations_cost_repair")
	require.NotContains(t, readMigrationSQL(t, "00002_auth.sql"), "crypto.network_operation.allow_editing")
	require.NotContains(t, sql, "manual_edit_allowed")
	require.NotContains(t, sql, "reg_tron_account_activations")
	require.NotContains(t, sql, "chain_activation_tx_hash")
	require.NotContains(t, sql, "chain_activation_error")
	require.NotContains(t, sql, "chain_activated_at")
	require.NotContains(t, sql, "chain_activation_checked_at")
	require.NotContains(t, platformCostSQL, "chain_account_activation")
	for _, technicalDuplicate := range []string{"hot_wallet_id", "source_address", "account_address", "cost_token_id", "cost_amount"} {
		activationTable := sql[strings.Index(sql, "CREATE TABLE reg_crypto_account_activations"):]
		activationTable = activationTable[:strings.Index(activationTable, ");")]
		require.NotContains(t, activationTable, technicalDuplicate)
	}
}

func TestWalletPoolMigrationUsesDurableCursorAndBudgetGate(t *testing.T) {
	sql := readMigrationSQL(t, "00006_crypto_core.sql")

	require.Contains(t, sql, "CREATE TABLE sys_crypto_wallet_pool_policies")
	require.Contains(t, sql, "CREATE TABLE sys_crypto_wallet_derivations")
	require.Contains(t, sql, "CREATE TABLE sys_crypto_wallet_derivation_cursors")
	require.Contains(t, sql, "CREATE TABLE sys_crypto_wallet_provisioning_jobs")
	require.Contains(t, sql, "INSERT INTO sys_crypto_wallet_derivations")
	require.Contains(t, sql, "REFERENCES sys_crypto_wallet_derivations")
	require.Contains(t, sql, "UNIQUE (network_id, derivation_path)")
	require.Contains(t, sql, "idx_cat_wallets_network_derivation_path")
	require.NotContains(t, sql, "ALTER TABLE reg_crypto_account_activations")
	require.Contains(t, sql, "claim_token")
	require.Contains(t, sql, "low_watermark < target_ready")
	require.Contains(t, sql, "target_ready <= max_total_wallets")
	require.Contains(t, sql, "chk_wallet_pool_activation_policy")
	require.Contains(t, sql, "max_concurrent_activations IS NULL")
	require.Contains(t, sql, "daily_activation_budget_minor IS NOT NULL")
	require.Contains(t, sql, "enabled, low_watermark, target_ready")
	require.Contains(t, sql, "SELECT id, FALSE")
}

func TestTRC20ReusableSweepReserveIsDefinedBySeed(t *testing.T) {
	migrationSQL := readMigrationSQL(t, "00006_crypto_core.sql")
	seed := readSQL(t, "cmd", "seed", "main.go")

	require.Contains(t, migrationSQL, "Token minor units retained on reusable deposit wallets")
	require.Contains(t, seed, `"10000000", 1, "1", usdtCurrencyIDVal`)
	require.Contains(t, seed, "reserve=1 raw unit")
}

func TestFinalCryptoSchemaLivesInOwningMigrations(t *testing.T) {
	networks := readMigrationSQL(t, "00006_crypto_core.sql")
	wallets := readMigrationSQL(t, "00006_crypto_core.sql")
	payments := readMigrationSQL(t, "00006_crypto_core.sql")
	signing := readMigrationSQL(t, "00007_crypto_outbound.sql")
	treasury := readMigrationSQL(t, "00007_crypto_outbound.sql")
	multichain := readMigrationSQL(t, "00006_crypto_core.sql")

	require.Contains(t, networks, "chain_family")
	require.Contains(t, networks, "network_identifier")
	require.Contains(t, networks, "finality_mode")
	require.Contains(t, wallets, "wallet_role")
	require.Contains(t, wallets, "chain_activation_status")
	require.Contains(t, wallets, "cooldown_until")
	require.Contains(t, payments, "idx_doc_crypto_payments_chain_event")
	require.Contains(t, payments, "event_kind")
	require.Contains(t, payments, "canonicality")
	require.Contains(t, payments, "source_nonce")
	require.Contains(t, signing, "unsigned_envelope  JSONB NOT NULL")
	require.Contains(t, signing, "asset_standard")
	require.Contains(t, signing, "schema_version     INT NOT NULL CHECK (schema_version IN (1,2))")
	require.Contains(t, signing, "generation         INT NOT NULL DEFAULT 1")
	require.Contains(t, signing, "replacement_lease_expires_at")
	require.Contains(t, signing, "CHECK (policy_version = chain_family || '-v1')")
	require.NotContains(t, signing, "DEFAULT 'tron-v1'")
	require.NotContains(t, signing, "chain_family       TEXT NOT NULL DEFAULT")
	require.NotContains(t, signing, "WHERE unsigned_fingerprint <> ''")
	require.Contains(t, treasury, "CREATE TABLE reg_crypto_revenue_reservations")
	require.Contains(t, multichain, "CREATE TABLE sys_crypto_external_resource_policies")
	require.Contains(t, multichain, "fee_funding_mode")
	require.NotContains(t, multichain, "RENAME TO reg_crypto_revenue_reservations")
	require.NotContains(t, multichain, "ALTER TABLE doc_crypto_payments")
	require.NotContains(t, multichain, "ALTER TABLE reg_crypto_signing_authorizations")
}

func TestMigrationsDeclareFinalSchemaWithoutTransitionalDDL(t *testing.T) {
	files := tenantMigrationFiles(t)
	// A later domain may only add constraints to an earlier domain's table:
	// foreign keys to its own tables and checks of its own rules.
	crossModuleFK := regexp.MustCompile(`^ALTER TABLE \w+ ADD CONSTRAINT \w+ (FOREIGN KEY|CHECK) `)
	for _, file := range files {
		up, _, ok := strings.Cut(readMigrationSQL(t, filepath.Base(file)), "-- +goose Down\n")
		require.True(t, ok, file)
		for _, line := range strings.Split(up, "\n") {
			if strings.HasPrefix(line, "ALTER TABLE ") {
				require.Regexp(t, crossModuleFK, line, file)
			}
			require.False(t, strings.HasPrefix(line, "DROP "), "%s: %s", file, line)
			require.False(t, strings.HasPrefix(line, "UPDATE "), "%s: %s", file, line)
		}
	}

	require.Contains(t, readMigrationSQL(t, "00004_settlement.sql"), "update_settlement_balance_on_insert")
}

func TestAddressReuseCooldownMigrationDefinesHistoryAndInbox(t *testing.T) {
	sql := readMigrationSQL(t, "00006_crypto_core.sql")
	wallets := readMigrationSQL(t, "00006_crypto_core.sql")
	poolPolicy := readMigrationSQL(t, "00006_crypto_core.sql")

	require.Contains(t, readMigrationSQL(t, "00001_core.sql"), "CREATE EXTENSION IF NOT EXISTS btree_gist")
	require.Contains(t, wallets, "cooldown_until TIMESTAMPTZ")
	require.Contains(t, poolPolicy, "address_reuse_cooldown_seconds")
	require.Contains(t, poolPolicy, "max_watcher_lag_seconds")
	require.Contains(t, poolPolicy, "lease_blocked_reason")
	require.Contains(t, poolPolicy, "address_reuse_cooldown_seconds = 0 OR max_watcher_lag_seconds > 0")
	require.NotContains(t, sql, "ALTER TABLE cat_wallets")
	require.NotContains(t, sql, "ALTER TABLE sys_crypto_wallet_pool_policies")
	require.Contains(t, sql, "CREATE TABLE reg_crypto_wallet_allocations")
	require.Contains(t, sql, "idx_crypto_wallet_allocations_invoice")
	require.Contains(t, sql, "excl_crypto_wallet_allocations_window")
	require.Contains(t, sql, "CREATE TABLE reg_crypto_inbound_chain_events")
	require.Contains(t, sql, "idx_crypto_inbound_chain_events_identity")
	require.Contains(t, sql, "match_status IN")
	require.Contains(t, sql, "'received','matched','unmatched','needs_review','ignored','poison'")
	require.NotContains(t, sql, "WalletStatusCooldown")
}

func TestEthereumSepoliaMigrationKeepsMultichainSafetyContracts(t *testing.T) {
	migrationSQL := readMigrationSQL(t, "00006_crypto_core.sql")
	networks := readMigrationSQL(t, "00006_crypto_core.sql")
	payments := readMigrationSQL(t, "00006_crypto_core.sql")
	signing := readMigrationSQL(t, "00007_crypto_outbound.sql")
	multichain := readMigrationSQL(t, "00006_crypto_core.sql")
	require.Contains(t, networks, "finality_mode")
	require.Contains(t, payments, "(network_id, tx_hash, event_kind, event_index)")
	require.Contains(t, migrationSQL, "reg_evm_nonce_cursors")
	require.Contains(t, migrationSQL, "reg_evm_nonce_leases")
	require.Contains(t, migrationSQL, "reg_evm_nonce_lease_events")
	require.Contains(t, signing, "replacement_lease_expires_at")
	require.Contains(t, migrationSQL, "sys_chain_watcher_block_history")
	require.Contains(t, migrationSQL, "reg_crypto_network_holds")
	require.Contains(t, multichain, "sys_crypto_external_resource_policies")
	require.NotContains(t, migrationSQL, "ALTER TABLE doc_crypto_payments")
	require.NotContains(t, migrationSQL, "ALTER TABLE reg_crypto_signing_authorizations")
	require.NotContains(t, migrationSQL, "sys_crypto_tron_resource_policies")
	require.Contains(t, migrationSQL, "ETHEREUM-SEPOLIA")
	require.Contains(t, migrationSQL, "11155111")
	require.Contains(t, migrationSQL, "0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB9")
	require.Contains(t, migrationSQL, "'confirmations_or_finalized','blue',FALSE")
	require.Contains(t, migrationSQL, "NUMERIC(78,0)")
	require.Equal(t, 2, strings.Count(migrationSQL, "ON CONFLICT (code) WHERE deletion_mark=FALSE DO NOTHING"),
		"catalog upserts must infer the partial code indexes explicitly")
	require.NotContains(t, migrationSQL, "FLOAT")
}

func TestCryptoWithdrawalAndOutflowCustodyBoundariesAreInBaseline(t *testing.T) {
	balanceSQL := readMigrationSQL(t, "00006_crypto_core.sql")
	withdrawalSQL := readMigrationSQL(t, "00007_crypto_outbound.sql")
	treasurySQL := readMigrationSQL(t, "00007_crypto_outbound.sql")
	platformCostSQL := readMigrationSQL(t, "00007_crypto_outbound.sql")
	reconciliationSQL := readMigrationSQL(t, "00007_crypto_outbound.sql")
	networkSQL := readMigrationSQL(t, "00006_crypto_core.sql")
	signingSQL := readMigrationSQL(t, "00007_crypto_outbound.sql")

	require.Contains(t, withdrawalSQL, "fn_reject_managed_withdrawal_destination")
	require.Contains(t, withdrawalSQL, "chk_withdrawal_address_external_destination")
	require.Contains(t, balanceSQL, "CREATE UNIQUE INDEX uq_crypto_custody_adjustment_balance_effect")
	require.Contains(t, balanceSQL, "WHERE recorder_type = 'CryptoCustodyAdjustment'")
	require.Contains(t, platformCostSQL, "'chain_network_fee','custody_loss'")
	require.Contains(t, platformCostSQL, "'native_funding','custody_loss'")
	require.Contains(t, platformCostSQL, "CREATE CONSTRAINT TRIGGER trg_crypto_custody_adjustment_posting_effects")
	require.Contains(t, platformCostSQL, "DEFERRABLE INITIALLY DEFERRED")
	require.Contains(t, platformCostSQL, "one platform custody-loss expense and no merchant movement")
	require.Contains(t, platformCostSQL, "CREATE TRIGGER trg_crypto_custody_wallet_effect_guard")
	require.Contains(t, platformCostSQL, "CREATE TRIGGER trg_crypto_custody_platform_effect_guard")
	require.Contains(t, platformCostSQL, "custody adjustment wallet movements are immutable")
	require.Contains(t, platformCostSQL, "custody adjustment platform movements are immutable")
	require.Contains(t, treasurySQL, "trg_reject_managed_treasury_destination")
	require.Contains(t, treasurySQL, "kind IN ('token_balance_mismatch','native_fee_balance_mismatch','external_outflow')")
	require.Contains(t, treasurySQL, "chk_external_outflow_matched_basis")
	require.Contains(t, treasurySQL, "matched basis does not prove the exact observed chain effect")
	require.Contains(t, treasurySQL, "JOIN reg_crypto_chain_effect_matches matched")
	require.Contains(t, treasurySQL, "matched.event_index=NEW.event_index")
	require.Contains(t, treasurySQL, "posted custody adjustment is required before resolution")
	require.Contains(t, treasurySQL, "custody adjustment requires a currently final unauthorized loss")
	require.Contains(t, treasurySQL, "custody loss chain effect is no longer final")
	require.Contains(t, treasurySQL, "fresh successful reconciliation is required before resolution")
	require.Contains(t, treasurySQL, "SELECT adjustment.decided_at")
	require.Contains(t, treasurySQL, "AND adjustment.status='posted'")
	require.Contains(t, treasurySQL, "resolved_by<>classified_by")
	require.Contains(t, treasurySQL, "chk_crypto_custody_adjustment_posting_version")
	require.Contains(t, treasurySQL, "uq_external_outflow_observation_identity")
	require.Contains(t, treasurySQL, "CREATE CONSTRAINT TRIGGER trg_external_outflow_incident_state")
	require.Contains(t, treasurySQL, "CREATE CONSTRAINT TRIGGER trg_external_outflow_reconciliation_state")
	require.Contains(t, treasurySQL, "resolved external outflow requires its exact resolved reconciliation incident")
	require.Contains(t, signingSQL, "reg_crypto_expected_chain_effects")
	require.Contains(t, signingSQL, "reg_crypto_chain_effect_matches")
	require.Contains(t, signingSQL, "UNIQUE (network_id, tx_hash, event_kind, event_index)")
	require.Contains(t, reconciliationSQL, "NEW.details IS DISTINCT FROM OLD.details")
	require.Contains(t, reconciliationSQL, "CREATE TABLE reg_crypto_external_outflow_observations")
	require.Contains(t, reconciliationSQL, "uq_crypto_external_outflow_observation")
	require.Contains(t, reconciliationSQL, "fk_crypto_external_outflow_observation_identity")
	require.Contains(t, reconciliationSQL, "trg_external_outflow_observations_immutable")
	require.Contains(t, reconciliationSQL, "CREATE TRIGGER trg_reconciliation_checks_immutable")
	require.Contains(t, networkSQL, "NEW.details IS DISTINCT FROM OLD.details")
	require.Contains(t, networkSQL, "'external_outflow'")
}

func TestMerchantNetworkPolicyRecoveryAndEnforcementAreInBaseline(t *testing.T) {
	merchantSQL := readMigrationSQL(t, "00006_crypto_core.sql")

	require.Contains(t, merchantSQL, "surface='portal' AND api_key_id IS NULL")
	require.Contains(t, merchantSQL, "REFERENCES cat_merchant_api_keys(id,merchant_id)")
	require.Contains(t, merchantSQL, "actor_type IN ('user','api_key','system','system_admin','request')")
	require.Contains(t, merchantSQL, "merchant network policy must pass through warn mode")
	require.Contains(t, merchantSQL, "enforce mode requires at least one active CIDR")
	require.Contains(t, merchantSQL, "merchant_network_policy_changed")
}

// tableSQL returns the CREATE TABLE statement of one table.
func tableSQL(t *testing.T, sql, table string) string {
	t.Helper()
	_, rest, ok := strings.Cut(sql, "CREATE TABLE "+table+" (")
	require.True(t, ok, table)
	body, _, ok := strings.Cut(rest, "\n);")
	require.True(t, ok, table)
	return body
}

// readMigrationSQL reads a tenant migration from the platform's or a
// configuration's directory.
func readMigrationSQL(t *testing.T, name string) string {
	t.Helper()
	for _, dir := range tenantMigrationDirs(t) {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return string(data)
		}
	}
	t.Fatalf("tenant migration %s not found", name)
	return ""
}

// tenantMigrationDirs lists db/migrations and every db/configuration/<name>.
func tenantMigrationDirs(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "..", "db")
	configurations, err := filepath.Glob(filepath.Join(root, "configuration", "*"))
	require.NoError(t, err)
	return append([]string{filepath.Join(root, "migrations")}, configurations...)
}

// tenantMigrationFiles lists every tenant migration file.
func tenantMigrationFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, dir := range tenantMigrationDirs(t) {
		matches, err := filepath.Glob(filepath.Join(dir, "*.sql"))
		require.NoError(t, err)
		files = append(files, matches...)
	}
	return files
}

func readSQL(t *testing.T, parts ...string) string {
	t.Helper()

	pathParts := append([]string{"..", "..", "..", "..", ".."}, parts...)
	path := filepath.Join(pathParts...)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestBlockchainNetworkColorsMatchThePalette(t *testing.T) {
	sql := readMigrationSQL(t, "00006_crypto_core.sql")
	allowed := []string{"''"}
	for _, choice := range types.PaletteChoices {
		allowed = append(allowed, "'"+string(choice.Color)+"'")
	}
	require.Contains(t, sql, "CHECK (color IN ("+strings.Join(allowed, ",")+"))",
		"the column must accept exactly the palette, in its order")
}
