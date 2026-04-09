package tenant

import "errors"

var (
	// ErrTenantNotFound is returned when tenant does not exist in meta-database.
	ErrTenantNotFound = errors.New("tenant not found")

	// ErrTenantNotActive is returned when tenant exists but is not active.
	ErrTenantNotActive = errors.New("tenant is not active")

	// ErrTenantMigrationFailed is returned when tenant is in migration_failed state.
	// Admin can retry or rollback the migration via Control Plane.
	ErrTenantMigrationFailed = errors.New("tenant migration failed, awaiting admin action")

	// ErrMaxPoolLimit is returned when tenant manager reached pool limit.
	ErrMaxPoolLimit = errors.New("max tenant pool limit reached")

	// ErrTenantVersionMismatch is returned when tenant belongs to a different
	// version group than the current server instance (cloud mode).
	// The reverse proxy should route this tenant to the correct instance.
	ErrTenantVersionMismatch = errors.New("tenant version group mismatch")
)
