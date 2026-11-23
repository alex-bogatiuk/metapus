package crypto_integration_repo

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

// LoadIngestionAlertSettings reads one tenant snapshot on each monitor tick.
// RPC budgets remain on their independently versioned client generations.
func LoadIngestionAlertSettings(ctx context.Context) (map[string]rpcconfig.IngestionAlertSettings, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT n.code,COALESCE(s.ingestion_alert_settings,'{}'::jsonb) FROM cat_blockchain_networks n LEFT JOIN sys_crypto_rpc_network_settings s ON s.network_id=n.id WHERE NOT n.deletion_mark AND n._deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load ingestion alert settings: %w", err)
	}
	defer rows.Close()
	result := make(map[string]rpcconfig.IngestionAlertSettings)
	for rows.Next() {
		var code string
		var raw []byte
		var settings rpcconfig.IngestionAlertSettings
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
