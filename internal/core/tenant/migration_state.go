// Package tenant — MigrationStateStore interface for pre-update version persistence.
// Used by TenantUpdater to enable rollback after failed migrations.
package tenant

import "context"

// MigrationState represents the saved state before a migration attempt.
type MigrationState struct {
	TenantID           string           `db:"tenant_id"`
	PreUpdateVersions  map[string]int64 `db:"pre_update_versions"` // dir → goose version
	LastError          string           `db:"last_error"`
	UpdatedAt          string           `db:"updated_at"`
}

// MigrationStateStore manages pre-update migration snapshots in meta-database.
// Implementations must be safe for concurrent use.
type MigrationStateStore interface {
	// EnsureTable creates the tenant_migration_state table if not exists.
	// Called once during server startup. Idempotent.
	EnsureTable(ctx context.Context) error

	// SavePreUpdateVersions stores the goose version snapshot taken before migration.
	// Uses UPSERT — safe to call multiple times for the same tenant.
	SavePreUpdateVersions(ctx context.Context, tenantID string, versions map[string]int64) error

	// GetPreUpdateVersions retrieves the saved snapshot for rollback.
	// Returns nil map if no snapshot exists.
	GetPreUpdateVersions(ctx context.Context, tenantID string) (map[string]int64, error)

	// SaveLastError stores the migration error message for UI display.
	SaveLastError(ctx context.Context, tenantID string, errMsg string) error

	// GetState returns the full migration state for a tenant (for status endpoint).
	// Returns nil if no state exists.
	GetState(ctx context.Context, tenantID string) (*MigrationState, error)

	// ClearState removes the migration state after successful migration or rollback.
	ClearState(ctx context.Context, tenantID string) error
}
