package integration_repo

import (
	"context"
	"fmt"
	"strings"

	dbassets "metapus/db"
	coreversion "metapus/internal/core/version"
	"metapus/internal/infrastructure/storage/postgres"
)

// UpgradeSchema runs only from the explicit operator command, never on startup.
// An incomplete previous/manual installation is rejected instead of guessed at.
func UpgradeSchema(ctx context.Context, apply bool) (bool, error) {
	tm := postgres.MustGetTxManager(ctx)
	var needed bool
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'))`); err != nil {
			return err
		}
		var version, count, columns int
		if err := q.QueryRow(ctx, `SELECT COALESCE(max(version_id),0) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil {
			return err
		}
		if version < 60 || version > coreversion.ExpectedSchemaVersion {
			return fmt.Errorf("integration upgrade requires a known tenant schema between 60 and %d (found %d)", coreversion.ExpectedSchemaVersion, version)
		}
		if err := q.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=ANY(ARRAY['cat_integration_accounts','cat_integration_pools','sys_integration_pool_accounts','sys_integration_account_state','sys_integration_pool_state','sys_crypto_rpc_endpoints','sys_crypto_rpc_network_settings'])`).Scan(&count); err != nil {
			return err
		}
		if err := q.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND (table_name='cat_blockchain_networks' AND column_name='rpc_config_source' OR table_name='cat_rate_sources' AND column_name='integration_pool_id')`).Scan(&columns); err != nil {
			return err
		}
		if count == 7 && columns == 2 {
			var err error
			providers, err := upgradeProviderConstraints(ctx, apply, version)
			if err != nil {
				return err
			}
			periods, err := upgradeManualPeriods(ctx, apply)
			if err != nil {
				return err
			}
			alerts, err := upgradeIngestionAlertSettings(ctx, apply)
			needed = providers || periods || alerts
			return err
		}
		if count != 0 || columns != 0 {
			return fmt.Errorf("partial integration schema detected; restore or repair explicitly")
		}
		needed = true
		if !apply {
			return nil
		}
		sql, err := dbassets.IntegrationAccountsUpgradeSQL()
		if err != nil {
			return err
		}
		if _, err = q.Exec(ctx, sql); err != nil {
			return fmt.Errorf("apply tenant integration schema: %w", err)
		}
		return nil
	})
	return needed, err
}

// Only known baseline definitions may be upgraded. Unknown constraints
// require an explicit repair rather than silently dropping extra restrictions.
func upgradeProviderConstraints(ctx context.Context, apply bool, schemaVersion int) (bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	oldValues := "'alchemy','coingecko_demo','tron_api','telegram','smtp','resource_api'"
	chainstackValues := "'alchemy','coingecko_demo','tron_api','chainstack','telegram','smtp','resource_api'"
	getblockValues := "'alchemy','coingecko_demo','tron_api','chainstack','getblock','telegram','smtp','resource_api'"
	infuraValues := "'alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','telegram','smtp','resource_api'"
	newValues := strings.Replace(infuraValues, "'infura',", "'infura','drpc',", 1)
	if schemaVersion >= 63 {
		newValues += ",'getblock_net'"
	}
	amlValues := infuraValues + ",'getblock_net'"
	if schemaVersion >= 74 {
		newValues += ",'testnet_aml'"
	}
	normalize := strings.NewReplacer(" ", "", "\n", "", "\t", "", "::text", "")
	needed := false
	for _, table := range []string{"cat_integration_accounts", "cat_integration_pools"} {
		constraint := table + "_provider_check"
		var definition string
		if err := q.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=to_regclass($1) AND conname=$2 AND contype='c' AND convalidated`, table, constraint).Scan(&definition); err != nil {
			return false, fmt.Errorf("inspect integration provider constraint: %w", err)
		}
		definition = normalize.Replace(definition)
		if definition == "CHECK((provider=ANY(ARRAY["+newValues+"])))" {
			continue
		}
		currentValues := infuraValues
		if schemaVersion >= 63 {
			currentValues += ",'getblock_net'"
		}
		if schemaVersion >= 74 {
			currentValues += ",'testnet_aml'"
		}
		knownBaseline := definition == "CHECK((provider=ANY(ARRAY["+currentValues+"])))" || definition == "CHECK((provider=ANY(ARRAY["+oldValues+"])))" ||
			definition == "CHECK((provider=ANY(ARRAY["+chainstackValues+"])))" ||
			definition == "CHECK((provider=ANY(ARRAY["+getblockValues+"])))" ||
			definition == "CHECK((provider=ANY(ARRAY["+infuraValues+"])))" ||
			(schemaVersion >= 74 && definition == "CHECK((provider=ANY(ARRAY["+amlValues+"])))")
		if !knownBaseline {
			return false, fmt.Errorf("unknown provider constraint on %s; repair explicitly", table)
		}
		needed = true
		if apply {
			// Identifiers and values above are fixed application constants.
			if _, err := q.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s, ADD CONSTRAINT %s CHECK(provider IN (%s))", table, constraint, constraint, newValues)); err != nil {
				return false, fmt.Errorf("upgrade integration provider constraint: %w", err)
			}
		}
	}
	return needed, nil
}

// Add only the known manual-period shape. Unknown partial schemas fail closed.
func upgradeManualPeriods(ctx context.Context, apply bool) (bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	const predicate = `((provider='drpc' AND credentials_expires_at IS NOT NULL AND manual_period_started_at IS NOT NULL AND manual_period_version>0 AND credentials_expires_at>manual_period_started_at) OR (provider<>'drpc' AND credentials_expires_at IS NULL AND manual_period_started_at IS NULL AND manual_period_version=0))`
	var count, valid int
	err := q.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE (column_name IN ('credentials_expires_at','manual_period_started_at') AND udt_name='timestamptz' AND is_nullable='YES' AND column_default IS NULL) OR (column_name='manual_period_version' AND udt_name='int8' AND is_nullable='NO' AND column_default='0')) FROM information_schema.columns WHERE table_schema=current_schema() AND ((table_name='cat_integration_accounts' AND column_name IN ('credentials_expires_at','manual_period_started_at','manual_period_version')) OR (table_name='sys_integration_account_state' AND column_name='manual_period_version'))`).Scan(&count, &valid)
	if err != nil {
		return false, err
	}
	if count == 0 {
		if !apply {
			return true, nil
		}
		_, err = q.Exec(ctx, `ALTER TABLE cat_integration_accounts ADD COLUMN credentials_expires_at TIMESTAMPTZ, ADD COLUMN manual_period_started_at TIMESTAMPTZ, ADD COLUMN manual_period_version BIGINT NOT NULL DEFAULT 0, ADD CONSTRAINT cat_integration_accounts_manual_period_check CHECK (`+predicate+`); ALTER TABLE sys_integration_account_state ADD COLUMN manual_period_version BIGINT NOT NULL DEFAULT 0, ADD CONSTRAINT sys_integration_account_state_manual_period_version_check CHECK(manual_period_version>=0)`)
		return true, err
	}
	if count != 4 || valid != 4 {
		return false, fmt.Errorf("partial or unknown manual integration period columns")
	}
	normalize := strings.NewReplacer(" ", "", "\n", "", "\t", "", "::text", "")
	checks := []struct{ table, name, want string }{
		{"cat_integration_accounts", "cat_integration_accounts_manual_period_check", `CHECK ((((provider = 'drpc'::text) AND (credentials_expires_at IS NOT NULL) AND (manual_period_started_at IS NOT NULL) AND (manual_period_version > 0) AND (credentials_expires_at > manual_period_started_at)) OR ((provider <> 'drpc'::text) AND (credentials_expires_at IS NULL) AND (manual_period_started_at IS NULL) AND (manual_period_version = 0))))`},
		{"sys_integration_account_state", "sys_integration_account_state_manual_period_version_check", `CHECK ((manual_period_version >= 0))`},
	}
	for _, check := range checks {
		var definition string
		if err := q.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=to_regclass($1) AND conname=$2 AND contype='c' AND convalidated`, check.table, check.name).Scan(&definition); err != nil {
			return false, fmt.Errorf("inspect manual period constraint: %w", err)
		}
		if normalize.Replace(definition) != normalize.Replace(check.want) {
			return false, fmt.Errorf("unknown manual period constraint on %s", check.table)
		}
	}
	return false, nil
}
