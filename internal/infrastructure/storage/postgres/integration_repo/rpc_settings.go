package integration_repo

import (
	"context"
	"fmt"

	"metapus/internal/core/json"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres"
)

// LoadIngestionAlertSettings reads one tenant snapshot on each monitor tick.
// RPC budgets remain on their independently versioned client generations.
func LoadIngestionAlertSettings(ctx context.Context) (map[string]integrations.IngestionAlertSettings, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT n.code,COALESCE(s.ingestion_alert_settings,'{}'::jsonb) FROM cat_blockchain_networks n LEFT JOIN sys_crypto_rpc_network_settings s ON s.network_id=n.id WHERE NOT n.deletion_mark AND n._deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load ingestion alert settings: %w", err)
	}
	defer rows.Close()
	result := make(map[string]integrations.IngestionAlertSettings)
	for rows.Next() {
		var code string
		var raw []byte
		var settings integrations.IngestionAlertSettings
		if err := rows.Scan(&code, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &settings); err != nil {
			return nil, fmt.Errorf("decode ingestion alert settings: %w", err)
		}
		if err := settings.Validate(); err != nil {
			return nil, err
		}
		result[code] = settings.Defaults()
	}
	return result, rows.Err()
}

// Existing tenants use the explicit integration-config upgrade command. No
// startup DDL and no edits to goose history or business/checkpoint records.
func upgradeIngestionAlertSettings(ctx context.Context, apply bool) (bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var count, valid int
	if err := q.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE udt_name='jsonb' AND is_nullable='NO' AND column_default='''{}''::jsonb') FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='sys_crypto_rpc_network_settings' AND column_name='ingestion_alert_settings'`).Scan(&count, &valid); err != nil {
		return false, err
	}
	if count == 0 {
		if !apply {
			return true, nil
		}
		_, err := q.Exec(ctx, `ALTER TABLE sys_crypto_rpc_network_settings ADD COLUMN ingestion_alert_settings JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(ingestion_alert_settings)='object')`)
		return true, err
	}
	if count != 1 || valid != 1 {
		return false, fmt.Errorf("unknown ingestion alert settings column")
	}
	var checked bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='sys_crypto_rpc_network_settings'::regclass AND conname='sys_crypto_rpc_network_settings_ingestion_alert_settings_check' AND contype='c' AND convalidated AND pg_get_constraintdef(oid)='CHECK ((jsonb_typeof(ingestion_alert_settings) = ''object''::text))')`).Scan(&checked); err != nil {
		return false, err
	}
	if !checked {
		return false, fmt.Errorf("unknown ingestion alert settings constraint")
	}
	return false, nil
}
