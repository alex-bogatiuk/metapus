package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	walletcatalog "metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"metapus/internal/core/json"
	"metapus/internal/core/version"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	nileUSDTContract     = "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf"
	sepoliaPYUSDContract = "0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB9"
)

func runConfigureDB(args []string) error {
	fs := flag.NewFlagSet("configure-db", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "initialized public test state directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" {
		return errors.New("--state-dir is required")
	}
	absState, err := filepath.Abs(*stateDir)
	if err != nil {
		return err
	}
	walletSets, err := readDerivedWalletSets(absState)
	if err != nil {
		return err
	}
	dsn, err := readSecret(filepath.Join(absState, secretDirName, "tenant_database_url"))
	if err != nil {
		return err
	}
	if dsn == "" {
		return errors.New("tenant database URL is empty; run finalize first")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse tenant database URL: %w", err)
	}
	config.MaxConns = 2
	config.MinConns = 0
	postgres.ConfigurePool(config)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("connect tenant database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping tenant database: %w", err)
	}

	// Wallet watch registration takes per-network advisory locks and requires
	// read committed; configure-db runs before any application writer starts.
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	keepOtherFamilies := len(walletSets) > 1
	for _, walletSet := range walletSets {
		if err := configureTenantDatabase(ctx, tx, absState, walletSet, keepOtherFamilies); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tenant crypto configuration: %w", err)
	}
	networks := make([]string, 0, len(walletSets))
	for _, walletSet := range walletSets {
		networks = append(networks, walletSet.Network)
	}
	fmt.Printf("Tenant database reconciled with the Vault liquidity, network-fee, and %s pool identities; existing wallet lifecycle state was preserved.\n", strings.Join(networks, " and "))
	return nil
}

func readDerivedWalletSets(stateDir string) ([]derivedWalletSet, error) {
	var sets []derivedWalletSet
	for _, name := range []string{"wallets-tron.json", "wallets-evm.json"} {
		path := filepath.Join(stateDir, name)
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", name, err)
		}
		walletSet, err := readDerivedWalletSetFile(path)
		if err != nil {
			return nil, err
		}
		sets = append(sets, walletSet)
	}
	if len(sets) > 0 {
		return sets, nil
	}
	walletSet, err := readDerivedWalletSet(stateDir)
	if err != nil {
		return nil, err
	}
	return []derivedWalletSet{walletSet}, nil
}

func readDerivedWalletSet(stateDir string) (derivedWalletSet, error) {
	return readDerivedWalletSetFile(filepath.Join(stateDir, "wallets.json"))
}

func readDerivedWalletSetFile(path string) (derivedWalletSet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return derivedWalletSet{}, fmt.Errorf("read derived wallets: %w", err)
	}
	var walletSet derivedWalletSet
	if err := json.Unmarshal(raw, &walletSet); err != nil {
		return derivedWalletSet{}, fmt.Errorf("decode derived wallets: %w", err)
	}
	if walletSet.TenantID == "" || (walletSet.Network != "tron_nile" && walletSet.Network != "ethereum_sepolia") || walletSet.AuthorizationKID == "" {
		return derivedWalletSet{}, errors.New("derived wallet set has incomplete tenant identity")
	}
	if err := validateDerivedWalletSet(walletSet); err != nil {
		return derivedWalletSet{}, err
	}
	return walletSet, nil
}

func validateDerivedWalletSet(walletSet derivedWalletSet) error {
	if len(walletSet.Wallets) != 4 {
		return fmt.Errorf("derived wallet set contains %d wallets, want exactly 4", len(walletSet.Wallets))
	}
	seenAddress := make(map[string]struct{}, len(walletSet.Wallets))
	seenPath := make(map[string]struct{}, len(walletSet.Wallets))
	roleCounts := make(map[walletcatalog.WalletRole]int, 3)
	for _, wallet := range walletSet.Wallets {
		if wallet.Code == "" || wallet.Address == "" || wallet.DerivationPath == "" || wallet.VaultKeyID == "" {
			return fmt.Errorf("derived wallet %q has incomplete identity", wallet.Code)
		}
		if _, exists := seenAddress[wallet.Address]; exists {
			return fmt.Errorf("duplicate derived address %s", wallet.Address)
		}
		seenAddress[wallet.Address] = struct{}{}
		if _, exists := seenPath[wallet.DerivationPath]; exists {
			return fmt.Errorf("duplicate derivation path %s", wallet.DerivationPath)
		}
		seenPath[wallet.DerivationPath] = struct{}{}
		switch wallet.Role {
		case walletcatalog.WalletRoleLiquidityHot, walletcatalog.WalletRoleNetworkFee:
			if wallet.Tier != walletcatalog.WalletTierHot {
				return fmt.Errorf("derived wallet role %q requires tier %q, got %q", wallet.Role, walletcatalog.WalletTierHot, wallet.Tier)
			}
		case walletcatalog.WalletRoleMerchantPool:
			if wallet.Tier != walletcatalog.WalletTierPool {
				return fmt.Errorf("derived wallet role %q requires tier %q, got %q", wallet.Role, walletcatalog.WalletTierPool, wallet.Tier)
			}
		default:
			return fmt.Errorf("unsupported derived wallet role %q", wallet.Role)
		}
		roleCounts[wallet.Role]++
	}
	if roleCounts[walletcatalog.WalletRoleLiquidityHot] != 1 ||
		roleCounts[walletcatalog.WalletRoleNetworkFee] != 1 ||
		roleCounts[walletcatalog.WalletRoleMerchantPool] != 2 {
		return fmt.Errorf(
			"derived wallet roles are liquidity_hot=%d network_fee=%d merchant_pool=%d, want 1/1/2",
			roleCounts[walletcatalog.WalletRoleLiquidityHot],
			roleCounts[walletcatalog.WalletRoleNetworkFee],
			roleCounts[walletcatalog.WalletRoleMerchantPool],
		)
	}
	return nil
}

func configureTenantDatabase(ctx context.Context, tx pgx.Tx, stateDir string, walletSet derivedWalletSet, keepOtherFamilies bool) error {
	var schema int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_id),0) FROM goose_db_version WHERE is_applied AND version_id<$1`, version.FirstExtensionMigration).Scan(&schema); err != nil {
		return fmt.Errorf("verify schema version: %w", err)
	}
	if !version.CompatibleSchema(schema) {
		return fmt.Errorf("tenant database schema %d is not the expected version %d", schema, version.ExpectedSchemaVersion)
	}

	networkCode, tokenCode, tokenStandard, expectedContract := "TRON-NILE", "USDT-TRC20", "TRC-20", nileUSDTContract
	if walletSet.Network == "ethereum_sepolia" {
		networkCode, tokenCode, tokenStandard, expectedContract = "ETHEREUM-SEPOLIA", "PYUSD-SEPOLIA", "ERC-20", sepoliaPYUSDContract
	}
	var networkID, contract string
	var decimals int
	if err := tx.QueryRow(ctx, `
		SELECT n.id::text, t.contract_address, t.decimal_places
		FROM cat_blockchain_networks n
		JOIN cat_tokens t ON t.network_id = n.id
		WHERE n.code = $1
		  AND n.deletion_mark = FALSE
		  AND t.code = $2
		  AND t.token_standard = $3
		  AND t.is_active = TRUE
		  AND t.deletion_mark = FALSE
	`, networkCode, tokenCode, tokenStandard).Scan(&networkID, &contract, &decimals); err != nil {
		return fmt.Errorf("verify %s catalog: %w", networkCode, err)
	}
	if !strings.EqualFold(contract, expectedContract) || decimals != 6 {
		return fmt.Errorf("unexpected %s identity: contract=%s decimals=%d", tokenCode, contract, decimals)
	}
	var nativeTokenCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM cat_tokens
		WHERE network_id = $1
		  AND token_standard = 'native'
		  AND is_active = TRUE
		  AND deletion_mark = FALSE
	`, networkID).Scan(&nativeTokenCount); err != nil {
		return fmt.Errorf("verify %s native token: %w", networkCode, err)
	}
	if nativeTokenCount != 1 {
		return fmt.Errorf("expected exactly one active %s native token, got %d", networkCode, nativeTokenCount)
	}
	if _, err := tx.Exec(ctx, `UPDATE cat_blockchain_networks SET is_active=TRUE,updated_at=NOW(),version=version+1 WHERE id=$1 AND is_active=FALSE`, networkID); err != nil {
		return fmt.Errorf("activate %s catalog: %w", networkCode, err)
	}
	if walletSet.Network == "ethereum_sepolia" {
		if !keepOtherFamilies {
			if _, err := tx.Exec(ctx, `UPDATE cat_blockchain_networks SET is_active=FALSE,updated_at=NOW(),version=version+1 WHERE code='TRON-NILE' AND is_active=TRUE`); err != nil {
				return fmt.Errorf("deactivate TRON catalog in Sepolia-only stand: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE cat_tokens SET sweep_threshold=1000000,sweep_max_age_hours=1,sweep_retain_amount=0,updated_at=NOW(),version=version+1 WHERE network_id=$1 AND code='PYUSD-SEPOLIA'`, networkID); err != nil {
			return fmt.Errorf("configure Sepolia PYUSD sweep threshold: %w", err)
		}
	}
	if walletSet.Network == "tron_nile" {
		if _, err := tx.Exec(ctx, `UPDATE cat_tokens SET sweep_threshold=1000000,sweep_max_age_hours=1,sweep_retain_amount=1,updated_at=NOW(),version=version+1 WHERE network_id=$1 AND code='USDT-TRC20'`, networkID); err != nil {
			return fmt.Errorf("configure Nile USDT sweep threshold: %w", err)
		}
	}

	if err := retireUnconfiguredPoolPlaceholders(ctx, tx, networkID); err != nil {
		return err
	}
	for _, wallet := range walletSet.Wallets {
		if err := upsertFreshWallet(ctx, tx, networkID, networkCode, wallet); err != nil {
			return err
		}
	}
	if walletSet.Network == "ethereum_sepolia" {
		if err := repairDerivedWalletActivationArtifacts(ctx, tx, networkID); err != nil {
			return err
		}
	}
	if err := configureWalletPool(ctx, tx, networkID, walletSet); err != nil {
		return err
	}
	if walletSet.Network == "ethereum_sepolia" {
		if err := configureSepoliaNetworkPolicy(ctx, tx, networkID); err != nil {
			return err
		}
	}
	if walletSet.Network == "tron_nile" {
		if err := configureNileNetworkPolicy(ctx, tx, networkID); err != nil {
			return err
		}
	}
	commandTag, err := tx.Exec(ctx, `
		INSERT INTO sys_merchant_users (user_id, merchant_id, role)
		SELECT u.id, m.id, 1
		FROM auth_users u
		CROSS JOIN cat_merchants m
		WHERE u.email = 'admin@metapus.io'
		  AND u.deletion_mark = FALSE
		  AND m.code = 'MERCHANT-001'
		  AND m.deletion_mark = FALSE
		ON CONFLICT (user_id, merchant_id) DO UPDATE
		SET role = EXCLUDED.role, updated_at = NOW()
	`)
	if err != nil {
		return fmt.Errorf("grant admin merchant ownership: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("admin@metapus.io or MERCHANT-001 is missing")
	}
	return nil
}

func configureWalletPool(
	ctx context.Context,
	tx pgx.Tx,
	networkID string,
	walletSet derivedWalletSet,
) error {
	namespace := ""
	var nextIndex int64
	for _, item := range walletSet.Wallets {
		if item.Tier != "pool" {
			continue
		}
		slash := strings.LastIndex(item.DerivationPath, "/")
		if slash <= 0 || slash == len(item.DerivationPath)-1 {
			return fmt.Errorf("invalid pool derivation path %q", item.DerivationPath)
		}
		itemNamespace := item.DerivationPath[:slash]
		if namespace == "" {
			namespace = itemNamespace
		} else if namespace != itemNamespace {
			return errors.New("public test pool wallets use ambiguous derivation namespaces")
		}
		index, err := strconv.ParseInt(item.DerivationPath[slash+1:], 10, 64)
		if err != nil || index < 0 {
			return fmt.Errorf("invalid pool derivation index in %q", item.DerivationPath)
		}
		if index >= nextIndex {
			nextIndex = index + 1
		}
	}
	if namespace == "" {
		return errors.New("public test pool derivation namespace is missing")
	}

	var fundingRecorded bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1
		    FROM reg_crypto_wallet_fundings f
		    JOIN cat_wallets w ON w.id = f.wallet_id
		    JOIN cat_tokens t ON t.id = f.token_id
		    WHERE f.network_id = $1
		      AND w.wallet_role = 'liquidity_hot'
		      AND t.token_standard = 'native'
		)
	`, networkID).Scan(&fundingRecorded); err != nil {
		return fmt.Errorf("check Nile hot-wallet funding ledger: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO sys_crypto_wallet_derivations (
		    network_id, derivation_path, wallet_id, source
		)
		SELECT network_id, derivation_path, id, 'bootstrap'
		FROM cat_wallets
		WHERE network_id = $1
		  AND derivation_path <> ''
		ON CONFLICT (network_id, derivation_path) DO UPDATE
		SET wallet_id = EXCLUDED.wallet_id,
		    version = sys_crypto_wallet_derivations.version + 1
		WHERE sys_crypto_wallet_derivations.wallet_id IS NULL
		   OR sys_crypto_wallet_derivations.wallet_id = EXCLUDED.wallet_id
	`, networkID); err != nil {
		return fmt.Errorf("publish Nile wallet derivations: %w", err)
	}
	var derivationIdentityConflicts int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM sys_crypto_wallet_derivations d
		JOIN cat_wallets w
		  ON w.network_id = d.network_id
		 AND w.derivation_path = d.derivation_path
		WHERE d.network_id = $1
		  AND d.wallet_id IS DISTINCT FROM w.id
	`, networkID).Scan(&derivationIdentityConflicts); err != nil {
		return fmt.Errorf("verify Nile wallet derivation registry: %w", err)
	}
	if derivationIdentityConflicts != 0 {
		return errors.New("nile wallet derivation path is already bound to a different wallet identity")
	}

	var maxPublishedIndex int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(
		    MAX(substring(d.derivation_path FROM '/([0-9]+)$')::BIGINT),
		    -1
		)
		FROM sys_crypto_wallet_derivations d
		JOIN cat_wallets w ON w.id = d.wallet_id
		WHERE d.network_id = $1
		  AND d.derivation_path LIKE ($2 || '/%')
		  AND w.tier = 'pool'
	`, networkID, namespace).Scan(&maxPublishedIndex); err != nil {
		return fmt.Errorf("read Nile published derivation indexes: %w", err)
	}
	if maxPublishedIndex >= nextIndex {
		nextIndex = maxPublishedIndex + 1
	}

	var conflictingActiveWallets, conflictingJobs int
	if err := tx.QueryRow(ctx, `
		SELECT
		    COUNT(*) FILTER (
		        WHERE w.id IS NOT NULL
		          AND w.derivation_path NOT LIKE ($2 || '/%')
		    ),
		    (
		        SELECT COUNT(*)
		        FROM sys_crypto_wallet_provisioning_jobs j
		        WHERE j.network_id = $1
		          AND j.derivation_path NOT LIKE ($2 || '/%')
		    )
		FROM cat_wallets w
		WHERE w.network_id = $1
		  AND w.tier = 'pool'
		  AND w.is_active = TRUE
		  AND w.deletion_mark = FALSE
	`, networkID, namespace).Scan(&conflictingActiveWallets, &conflictingJobs); err != nil {
		return fmt.Errorf("check Nile derivation generations: %w", err)
	}
	canAdoptNamespace := conflictingActiveWallets == 0 && conflictingJobs == 0

	var existingNamespace string
	var existingNextIndex int64
	err := tx.QueryRow(ctx, `
		SELECT derivation_namespace, next_index
		FROM sys_crypto_wallet_derivation_cursors
		WHERE network_id = $1
		FOR UPDATE
	`, networkID).Scan(&existingNamespace, &existingNextIndex)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if _, err = tx.Exec(ctx, `
				INSERT INTO sys_crypto_wallet_derivation_cursors (
			    network_id, derivation_namespace, next_index, needs_review
			) VALUES ($1, $2, $3, $4)
		`, networkID, namespace, nextIndex, !canAdoptNamespace); err != nil {
			return fmt.Errorf("insert Nile derivation cursor: %w", err)
		}
	case err != nil:
		return fmt.Errorf("lock Nile derivation cursor: %w", err)
	case existingNamespace == namespace:
		if existingNextIndex > nextIndex {
			nextIndex = existingNextIndex
		}
		if _, err = tx.Exec(ctx, `
				UPDATE sys_crypto_wallet_derivation_cursors
				SET next_index = $2, needs_review = $3, version = version + 1
			WHERE network_id = $1
		`, networkID, nextIndex, !canAdoptNamespace); err != nil {
			return fmt.Errorf("update Nile derivation cursor: %w", err)
		}
	case canAdoptNamespace:
		// The former namespace only belonged to retired, history-free seed
		// placeholders. Switching generations cannot reuse any published path.
		if _, err = tx.Exec(ctx, `
				UPDATE sys_crypto_wallet_derivation_cursors
				SET derivation_namespace = $2, next_index = $3,
				    needs_review = FALSE, version = version + 1
			WHERE network_id = $1
		`, networkID, namespace, nextIndex); err != nil {
			return fmt.Errorf("adopt Nile derivation namespace: %w", err)
		}
	default:
		if _, err = tx.Exec(ctx, `
				UPDATE sys_crypto_wallet_derivation_cursors
				SET needs_review = TRUE, version = version + 1
			WHERE network_id = $1
		`, networkID); err != nil {
			return fmt.Errorf("hold ambiguous Nile derivation cursor: %w", err)
		}
	}

	var storedNamespace string
	var needsReview bool
	if err := tx.QueryRow(ctx, `
		SELECT derivation_namespace, needs_review
		FROM sys_crypto_wallet_derivation_cursors
		WHERE network_id = $1
	`, networkID).Scan(&storedNamespace, &needsReview); err != nil {
		return fmt.Errorf("read Nile derivation cursor: %w", err)
	}
	lastErrorCode := ""
	lastErrorMessage := ""
	if !fundingRecorded {
		lastErrorCode = "FUNDING_RECEIPT_REQUIRED"
		lastErrorMessage = "Требуется подтверждённый funding receipt для native liquidity hot wallet."
	} else if needsReview {
		lastErrorCode = "DERIVATION_REVIEW_REQUIRED"
		lastErrorMessage = "Derivation namespace изменился и требует проверки."
	}
	activation := walletPoolActivationParamsFor(walletSet.Network)
	if _, err := tx.Exec(ctx, `
		INSERT INTO sys_crypto_wallet_pool_policies (
		    network_id, enabled, low_watermark, target_ready, max_total_wallets,
		    max_provision_batch, max_concurrent_activations,
		    max_active_invoice_leases_per_merchant,
		    max_persistent_addresses_per_merchant,
		    max_lease_starts_per_minute_per_merchant,
		    min_global_ready_reserve,
		    activation_reserve_minor, min_hot_native_balance_minor,
		    daily_activation_budget_minor, retry_after_seconds,
		    address_reuse_cooldown_seconds, max_watcher_lag_seconds,
		    derivation_namespace, needs_review, updated_by,
		    last_error_code, last_error_message
		) VALUES (
		    $1, $2, 2, 5, 20,
		    5, $7,
		    5, 5, 20, 1,
		    $8, $9,
		    $10, 30,
		    86400, 300,
		    $3, $6, 'public-test-bootstrap', $4, $5
		)
		ON CONFLICT (network_id) DO UPDATE
		SET enabled = EXCLUDED.enabled,
		    low_watermark = EXCLUDED.low_watermark,
		    target_ready = EXCLUDED.target_ready,
		    max_total_wallets = EXCLUDED.max_total_wallets,
		    max_provision_batch = EXCLUDED.max_provision_batch,
		    max_concurrent_activations = EXCLUDED.max_concurrent_activations,
		    max_active_invoice_leases_per_merchant = EXCLUDED.max_active_invoice_leases_per_merchant,
		    max_persistent_addresses_per_merchant = EXCLUDED.max_persistent_addresses_per_merchant,
		    max_lease_starts_per_minute_per_merchant = EXCLUDED.max_lease_starts_per_minute_per_merchant,
		    min_global_ready_reserve = EXCLUDED.min_global_ready_reserve,
		    activation_reserve_minor = EXCLUDED.activation_reserve_minor,
		    min_hot_native_balance_minor = EXCLUDED.min_hot_native_balance_minor,
		    daily_activation_budget_minor = EXCLUDED.daily_activation_budget_minor,
		    retry_after_seconds = EXCLUDED.retry_after_seconds,
		    address_reuse_cooldown_seconds = EXCLUDED.address_reuse_cooldown_seconds,
		    max_watcher_lag_seconds = EXCLUDED.max_watcher_lag_seconds,
		    derivation_namespace = EXCLUDED.derivation_namespace,
		    needs_review = EXCLUDED.needs_review,
		    updated_by = EXCLUDED.updated_by,
		    last_error_code = EXCLUDED.last_error_code,
		    last_error_message = EXCLUDED.last_error_message,
		    version = sys_crypto_wallet_pool_policies.version + 1
		WHERE sys_crypto_wallet_pool_policies.updated_by IN ('', 'public-test-bootstrap')
	`, networkID, fundingRecorded && !needsReview, storedNamespace,
		lastErrorCode, lastErrorMessage, needsReview, activation[0],
		activation[1], activation[2], activation[3]); err != nil {
		return fmt.Errorf("configure Nile wallet pool policy: %w", err)
	}
	return nil
}

func walletPoolActivationParamsFor(network string) [4]any {
	if network == "tron_nile" {
		return [4]any{1, 2000000, 2000000, 10000000}
	}
	return [4]any{}
}

func configureNileNetworkPolicy(ctx context.Context, tx pgx.Tx, networkID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE sys_crypto_network_policies
		SET enabled=FALSE,
		    per_transaction_max_fee_minor='15000000',
		    daily_network_fee_budget_minor='200000000',
		    fee_funding_mode='jit',
		    daily_jit_funding_budget_minor='50000000',
		    min_fee_wallet_balance_minor='20000000',
		    min_source_wallet_residual_minor='1000000',
		    jit_funding_max_amount_minor='20000000',
		    retry_delay_seconds=15,
		    rpc_failure_threshold=3,
		    rpc_open_seconds=30,
		    updated_by='public-test-bootstrap',
		    version=version+1
		WHERE network_id=$1
		  AND _deleted_at IS NULL
		  AND updated_by IN ('', 'public-test-bootstrap')
	`, networkID); err != nil {
		return fmt.Errorf("configure Nile network policy: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sys_crypto_external_resource_policies (
		    network_id, resource_strategy, provider_account_ids, provider_failure_threshold, provider_open_seconds
		) VALUES ($1, 'burn_only', ARRAY[]::uuid[], 3, 60)
		ON CONFLICT (network_id) DO NOTHING
	`, networkID); err != nil {
		return fmt.Errorf("configure Nile resource policy: %w", err)
	}
	return nil
}

func configureSepoliaNetworkPolicy(ctx context.Context, tx pgx.Tx, networkID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE sys_crypto_network_policies
		SET enabled=FALSE,
		    per_transaction_max_fee_minor='3000000000000000',
		    daily_network_fee_budget_minor='50000000000000000',
		    fee_funding_mode='jit',
		    daily_jit_funding_budget_minor='40000000000000000',
		    min_fee_wallet_balance_minor='5000000000000000',
		    min_source_wallet_residual_minor='100000000000000',
		    jit_funding_max_amount_minor='4000000000000000',
		    retry_delay_seconds=15,
		    rpc_failure_threshold=3,
		    rpc_open_seconds=30,
		    updated_by='public-test-bootstrap',
		    version=version+1
		WHERE network_id=$1
		  AND _deleted_at IS NULL
		  AND updated_by IN ('', 'public-test-bootstrap')
	`, networkID); err != nil {
		return fmt.Errorf("configure Sepolia network policy: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sys_crypto_external_resource_policies WHERE network_id=$1`, networkID); err != nil {
		return fmt.Errorf("remove incompatible Sepolia resource policy: %w", err)
	}
	return nil
}

func retireUnconfiguredPoolPlaceholders(ctx context.Context, tx pgx.Tx, networkID string) error {
	rows, err := tx.Query(ctx, `
		SELECT id::text, code, deletion_mark
		FROM cat_wallets
		WHERE network_id = $1
		  AND tier = 'pool'
		  AND vault_key_id = ''
		FOR UPDATE
	`, networkID)
	if err != nil {
		return fmt.Errorf("list unconfigured pool placeholders: %w", err)
	}
	type placeholder struct {
		id      string
		code    string
		deleted bool
	}
	var placeholders []placeholder
	for rows.Next() {
		var item placeholder
		if err := rows.Scan(&item.id, &item.code, &item.deleted); err != nil {
			rows.Close()
			return fmt.Errorf("scan unconfigured pool placeholder: %w", err)
		}
		placeholders = append(placeholders, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate unconfigured pool placeholders: %w", err)
	}
	rows.Close()

	for _, item := range placeholders {
		hasHistory, err := walletHasHistory(ctx, tx, item.id)
		if err != nil {
			return err
		}
		if hasHistory {
			return fmt.Errorf("unconfigured pool placeholder %s has financial history and cannot be retired", item.code)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE reg_crypto_reconciliation_items
			SET status = 'resolved',
			    resolved_at = NOW(),
			    resolution_reason = 'retired_unconfigured_seed_placeholder',
			    updated_at = NOW()
			WHERE wallet_id = $1
			  AND status IN ('open','acknowledged')
		`, item.id); err != nil {
			return fmt.Errorf("resolve placeholder incident %s: %w", item.code, err)
		}
		if !item.deleted {
			if _, err := tx.Exec(ctx, `
				UPDATE cat_wallets
				SET is_active = FALSE,
				    deletion_mark = TRUE,
				    address = 'retired:' || id::text,
				    version = version + 1
				WHERE id = $1
			`, item.id); err != nil {
				return fmt.Errorf("retire unconfigured pool placeholder %s: %w", item.code, err)
			}
		} else if _, err := tx.Exec(ctx, `
			UPDATE cat_wallets
			SET address = 'retired:' || id::text,
			    version = version + 1
			WHERE id = $1
			  AND address NOT LIKE 'retired:%'
		`, item.id); err != nil {
			return fmt.Errorf("release retired pool placeholder address %s: %w", item.code, err)
		}
	}
	return nil
}

func upsertFreshWallet(ctx context.Context, tx pgx.Tx, networkID, networkCode string, wallet derivedWallet) error {
	networkLabel := strings.ReplaceAll(networkCode, "-", " ")
	poolActivationStatus := poolActivationStatusFor(networkCode)
	var existingID, existingNetworkID, existingAddress, existingPath, existingVaultKeyID string
	err := tx.QueryRow(ctx, `
		SELECT id::text, network_id::text, address, derivation_path, vault_key_id
		FROM cat_wallets
		WHERE code = $1 AND deletion_mark = FALSE
		FOR UPDATE
	`, wallet.Code).Scan(
		&existingID,
		&existingNetworkID,
		&existingAddress,
		&existingPath,
		&existingVaultKeyID,
	)
	identityChanged := false
	switch {
	case err == nil:
		identityChanged = existingNetworkID != networkID ||
			existingAddress != wallet.Address ||
			existingPath != wallet.DerivationPath ||
			existingVaultKeyID != wallet.VaultKeyID
		if identityChanged {
			hasHistory, historyErr := walletHasHistory(ctx, tx, existingID)
			if historyErr != nil {
				return historyErr
			}
			if hasHistory {
				return fmt.Errorf("wallet %s has financial history and cannot be rebound; choose a new pool account", wallet.Code)
			}
		}
	case errors.Is(err, pgx.ErrNoRows):
		existingID = ""
	default:
		return fmt.Errorf("read existing wallet %s: %w", wallet.Code, err)
	}

	var poolHoldDetails []byte
	switch wallet.Role {
	case walletcatalog.WalletRoleLiquidityHot:
		if existingID == "" {
			_, err = tx.Exec(ctx, `
				INSERT INTO cat_wallets (
					code, name, network_id, address, derivation_path, vault_key_id,
					tier, wallet_role, status, allocation_mode, is_active,
					chain_activation_status
				) VALUES (
					$1, $6, $2, $3, $4, $5,
					'hot', 'liquidity_hot', 'free', 'transient', TRUE,
					'active'
				)
			`, wallet.Code, networkID, wallet.Address, wallet.DerivationPath, wallet.VaultKeyID, networkLabel+" Hot Wallet")
		} else if !identityChanged {
			_, err = tx.Exec(ctx, `
				UPDATE cat_wallets
				SET name = $2,
				    tier = 'hot', wallet_role = 'liquidity_hot',
				    is_active = TRUE
				WHERE id = $1
			`, existingID, networkLabel+" Hot Wallet")
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE cat_wallets
				SET network_id = $2, address = $3, derivation_path = $4, vault_key_id = $5,
				    tier = 'hot', wallet_role = 'liquidity_hot', status = 'free', is_active = TRUE,
				    chain_activation_status = 'active', version = version + 1
				WHERE id = $1
			`, existingID, networkID, wallet.Address, wallet.DerivationPath, wallet.VaultKeyID)
		}
	case walletcatalog.WalletRoleNetworkFee:
		if existingID == "" {
			_, err = tx.Exec(ctx, `
				INSERT INTO cat_wallets (
					code, name, network_id, address, derivation_path, vault_key_id,
					tier, wallet_role, status, allocation_mode, is_active,
					chain_activation_status
				) VALUES (
					$1, $6, $2, $3, $4, $5,
					'hot', 'network_fee', 'free', 'transient', TRUE,
					'active'
				)
			`, wallet.Code, networkID, wallet.Address, wallet.DerivationPath, wallet.VaultKeyID, networkLabel+" Network Fee Wallet")
		} else if !identityChanged {
			_, err = tx.Exec(ctx, `
				UPDATE cat_wallets
				SET name = $2,
				    tier = 'hot', wallet_role = 'network_fee',
				    status = 'free', allocation_mode = 'transient',
				    merchant_id = NULL, customer_id = '', is_active = TRUE,
				    chain_activation_status = 'active'
				WHERE id = $1
			`, existingID, networkLabel+" Network Fee Wallet")
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE cat_wallets
				SET network_id = $2, address = $3, derivation_path = $4, vault_key_id = $5,
				    name = $6,
				    tier = 'hot', wallet_role = 'network_fee', status = 'free', allocation_mode = 'transient',
				    merchant_id = NULL, customer_id = '', is_active = TRUE,
				    chain_activation_status = 'active', version = version + 1
				WHERE id = $1
			`, existingID, networkID, wallet.Address, wallet.DerivationPath, wallet.VaultKeyID, networkLabel+" Network Fee Wallet")
		}
	case walletcatalog.WalletRoleMerchantPool:
		holdDetails, marshalErr := json.Marshal(map[string]string{"derivationPath": wallet.DerivationPath})
		if marshalErr != nil {
			return marshalErr
		}
		poolHoldDetails = holdDetails
		if existingID == "" {
			err = tx.QueryRow(ctx, `
				INSERT INTO cat_wallets (
					code, name, network_id, address, derivation_path, vault_key_id,
					tier, wallet_role, status, allocation_mode, is_active,
					chain_activation_status
				) VALUES (
					$1, $2, $3, $4, $5, $6,
					'pool', 'merchant_pool', 'free', 'transient', TRUE,
					$7
				)
				RETURNING id
			`, wallet.Code, networkLabel+" Pool "+wallet.Code, networkID, wallet.Address, wallet.DerivationPath, wallet.VaultKeyID, poolActivationStatus).Scan(&existingID)
		} else if !identityChanged {
			_, err = tx.Exec(ctx, `
				UPDATE cat_wallets
				SET name = $2,
				    tier = 'pool', wallet_role = 'merchant_pool',
				    is_active = TRUE,
				    chain_activation_status = CASE
				        WHEN $3 = 'active' THEN $3
				        ELSE chain_activation_status
				    END
				WHERE id = $1
			`, existingID, networkLabel+" Pool "+wallet.Code, poolActivationStatus)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE cat_wallets
				SET network_id = $2, address = $3, derivation_path = $4, vault_key_id = $5,
				    tier = 'pool', wallet_role = 'merchant_pool', status = 'free', allocation_mode = 'transient',
				    merchant_id = NULL, customer_id = '', is_active = TRUE,
				    chain_activation_status = CASE
					WHEN $6 = 'active' THEN $6
					WHEN address IS DISTINCT FROM $3 THEN 'unknown'
					ELSE chain_activation_status
				END,
				version = version + 1
			WHERE id = $1
			`, existingID, networkID, wallet.Address, wallet.DerivationPath, wallet.VaultKeyID, poolActivationStatus)
		}
	default:
		return fmt.Errorf("bind wallet %s: unsupported role %q", wallet.Code, wallet.Role)
	}
	if err != nil {
		return fmt.Errorf("bind wallet %s: %w", wallet.Code, err)
	}
	if wallet.Role == walletcatalog.WalletRoleMerchantPool {
		if _, err := tx.Exec(ctx, `
			INSERT INTO reg_crypto_wallet_allocation_holds (
				wallet_id,reason,owner_type,owner_id,details,held_by
			) VALUES ($1::uuid,'chain_balance_check_pending','wallet_reconciliation',$1::uuid::text,$2::jsonb,'public-test-bootstrap')
			ON CONFLICT (wallet_id,owner_type,owner_id) WHERE status='open'
			DO UPDATE SET details=EXCLUDED.details
		`, existingID, poolHoldDetails); err != nil {
			return fmt.Errorf("hold bootstrap wallet pending reconciliation: %w", err)
		}
	}
	return nil
}

func poolActivationStatusFor(networkCode string) walletcatalog.ChainActivationStatus {
	if networkCode == "ETHEREUM-SEPOLIA" {
		return walletcatalog.ChainActivationActive
	}
	return walletcatalog.ChainActivationUnknown
}

func repairDerivedWalletActivationArtifacts(ctx context.Context, tx pgx.Tx, networkID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE reg_crypto_account_activations a
		SET status = 'active',
		    error_message = '',
		    activation_cost_reserved_minor = 0,
		    activation_cost_reserved_at = NULL,
		    activation_claimed_by = '',
		    activation_claim_token = NULL,
		    activation_lease_expires_at = NULL,
		    cost_status = 'not_applicable',
		    cost_checked_at = NOW(),
		    cost_error = '',
		    cost_claimed_by = '',
		    cost_claimed_at = NULL,
		    cost_lease_expires_at = NULL,
		    confirmed_at = COALESCE(confirmed_at, NOW()),
		    updated_at = NOW()
		FROM cat_wallets w
		WHERE a.wallet_id = w.id
		  AND a.network_id = $1
		  AND w.network_id = $1
		  AND a.network_operation_id IS NULL
		  AND a.status IN ('claimed', 'held')
		  AND a.activation_cost_reserved_minor = 0
		  AND a.activation_cost_reserved_at IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_signing_authorizations s
		      WHERE s.basis_id = a.id
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_fee_reservations f
		      WHERE f.basis_id = a.id
		  )
	`, networkID); err != nil {
		return fmt.Errorf("repair derived-family activation artifacts: %w", err)
	}
	return nil
}

func walletHasHistory(ctx context.Context, tx pgx.Tx, walletID string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM reg_crypto_balance_movements WHERE wallet_id = $1) OR
			EXISTS (SELECT 1 FROM reg_crypto_balance_balances WHERE wallet_id = $1 AND amount <> 0) OR
			EXISTS (SELECT 1 FROM doc_crypto_invoices WHERE wallet_id = $1) OR
			EXISTS (SELECT 1 FROM reg_crypto_sweep_attempts WHERE wallet_id = $1 OR hot_wallet_id = $1) OR
			EXISTS (SELECT 1 FROM doc_crypto_withdrawals WHERE source_wallet_id = $1) OR
			EXISTS (SELECT 1 FROM doc_treasury_withdrawals WHERE source_wallet_id = $1) OR
			EXISTS (SELECT 1 FROM reg_crypto_account_activations WHERE wallet_id = $1) OR
			EXISTS (SELECT 1 FROM doc_crypto_network_operations WHERE target_wallet_id = $1 OR source_wallet_id = $1) OR
			EXISTS (SELECT 1 FROM reg_crypto_fee_reservations WHERE payer_wallet_id = $1) OR
			EXISTS (SELECT 1 FROM doc_crypto_native_fundings WHERE source_wallet_id = $1 OR destination_wallet_id = $1)
	`, walletID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("inspect wallet financial history: %w", err)
	}
	return exists, nil
}
