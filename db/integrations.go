package dbassets

import (
	"fmt"
	"strings"
)

// IntegrationAccountsUpgradeSQL reuses the owning baseline migrations. Applying
// it is an explicit operator action; goose history and business data are untouched.
func IntegrationAccountsUpgradeSQL() (string, error) {
	section := func(file, marker string) (string, error) {
		raw, err := Files.ReadFile(file)
		if err != nil {
			return "", err
		}
		_, rest, ok := strings.Cut(string(raw), marker)
		if !ok {
			return "", fmt.Errorf("integration schema marker missing")
		}
		ddl, _, ok := strings.Cut(rest, "-- +goose Down")
		if !ok {
			return "", fmt.Errorf("integration schema section incomplete")
		}
		return ddl, nil
	}
	accounts, err := section("migrations/00012_sys_service_accounts.sql", "-- Tenant-local integration accounts and ordered pools. No plaintext credentials.")
	if err != nil {
		return "", err
	}
	rpc, err := section("migrations/00022_crypto_catalogs.sql", "-- INTEGRATION_RPC_SCHEMA_BEGIN")
	if err != nil {
		return "", err
	}
	return accounts + rpc + `
ALTER TABLE cat_blockchain_networks ADD COLUMN rpc_config_source TEXT NOT NULL DEFAULT 'file' CHECK(rpc_config_source IN ('file','database'));
ALTER TABLE cat_rate_sources ADD COLUMN integration_pool_id UUID REFERENCES cat_integration_pools(id) ON DELETE RESTRICT;
`, nil
}
