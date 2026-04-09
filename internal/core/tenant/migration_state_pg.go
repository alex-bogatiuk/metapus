package tenant

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresMigrationStateStore implements MigrationStateStore using the meta-database.
// Table tenant_migration_state is created automatically on first use (EnsureTable).
type PostgresMigrationStateStore struct {
	pool *pgxpool.Pool
}

// NewPostgresMigrationStateStore creates a new store backed by meta-database.
func NewPostgresMigrationStateStore(pool *pgxpool.Pool) *PostgresMigrationStateStore {
	return &PostgresMigrationStateStore{pool: pool}
}

// EnsureTable creates the tenant_migration_state table if it does not exist.
// Safe to call on every server startup — fully idempotent.
func (s *PostgresMigrationStateStore) EnsureTable(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS tenant_migration_state (
			tenant_id           UUID PRIMARY KEY REFERENCES tenants(id),
			pre_update_versions JSONB NOT NULL DEFAULT '{}',
			last_error          TEXT NOT NULL DEFAULT '',
			updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure tenant_migration_state table: %w", err)
	}
	return nil
}

func (s *PostgresMigrationStateStore) SavePreUpdateVersions(ctx context.Context, tenantID string, versions map[string]int64) error {
	versionsJSON, err := json.Marshal(versions)
	if err != nil {
		return fmt.Errorf("marshal pre_update_versions: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO tenant_migration_state (tenant_id, pre_update_versions, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (tenant_id) DO UPDATE
		SET pre_update_versions = EXCLUDED.pre_update_versions,
		    last_error = '',
		    updated_at = NOW()
	`, tenantID, versionsJSON)
	if err != nil {
		return fmt.Errorf("save pre_update_versions for %s: %w", tenantID, err)
	}
	return nil
}

func (s *PostgresMigrationStateStore) GetPreUpdateVersions(ctx context.Context, tenantID string) (map[string]int64, error) {
	var versionsJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT pre_update_versions FROM tenant_migration_state WHERE tenant_id = $1
	`, tenantID).Scan(&versionsJSON)
	if err != nil {
		// No row means no snapshot saved
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("get pre_update_versions for %s: %w", tenantID, err)
	}

	var versions map[string]int64
	if err := json.Unmarshal(versionsJSON, &versions); err != nil {
		return nil, fmt.Errorf("unmarshal pre_update_versions: %w", err)
	}
	return versions, nil
}

func (s *PostgresMigrationStateStore) SaveLastError(ctx context.Context, tenantID string, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE tenant_migration_state
		SET last_error = $2, updated_at = NOW()
		WHERE tenant_id = $1
	`, tenantID, errMsg)
	if err != nil {
		return fmt.Errorf("save last_error for %s: %w", tenantID, err)
	}
	return nil
}

func (s *PostgresMigrationStateStore) GetState(ctx context.Context, tenantID string) (*MigrationState, error) {
	var state MigrationState
	var versionsJSON []byte

	err := s.pool.QueryRow(ctx, `
		SELECT tenant_id, pre_update_versions, last_error, updated_at::text
		FROM tenant_migration_state
		WHERE tenant_id = $1
	`, tenantID).Scan(&state.TenantID, &versionsJSON, &state.LastError, &state.UpdatedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("get migration state for %s: %w", tenantID, err)
	}

	if err := json.Unmarshal(versionsJSON, &state.PreUpdateVersions); err != nil {
		return nil, fmt.Errorf("unmarshal pre_update_versions: %w", err)
	}
	return &state, nil
}

func (s *PostgresMigrationStateStore) ClearState(ctx context.Context, tenantID string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM tenant_migration_state WHERE tenant_id = $1
	`, tenantID)
	if err != nil {
		return fmt.Errorf("clear migration state for %s: %w", tenantID, err)
	}
	return nil
}

// Compile-time interface check.
var _ MigrationStateStore = (*PostgresMigrationStateStore)(nil)
