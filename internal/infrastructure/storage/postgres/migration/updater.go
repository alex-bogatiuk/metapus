// Package migration provides schema migration tools for tenant databases.
package migration

import (
	"context"
	"fmt"
	"sync"

	"metapus/internal/core/tenant"
	"metapus/internal/core/version"
	"metapus/pkg/logger"
)

// TenantUpdater manages background schema migrations triggered from UI.
// It ensures only one migration per tenant runs at a time and handles
// the full lifecycle: status → updating, evict pool, snapshot versions,
// run goose, restore status (or transition to migration_failed).
type TenantUpdater struct {
	registry   tenant.Registry
	manager    *tenant.Manager
	stateStore tenant.MigrationStateStore
	log        *logger.Logger

	// running tracks tenants currently being updated (prevents double-trigger).
	running sync.Map // map[tenantID]bool

	// Lifecycle context for background goroutines.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewTenantUpdater creates a new updater.
func NewTenantUpdater(
	registry tenant.Registry,
	manager *tenant.Manager,
	stateStore tenant.MigrationStateStore,
	log *logger.Logger,
) *TenantUpdater {
	ctx, cancel := context.WithCancel(context.Background())
	return &TenantUpdater{
		registry:   registry,
		manager:    manager,
		stateStore: stateStore,
		log:        log.WithComponent("tenant-updater"),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// IsUpdating returns true if the tenant is currently being migrated.
func (u *TenantUpdater) IsUpdating(tenantID string) bool {
	_, ok := u.running.Load(tenantID)
	return ok
}

// StateStore returns the migration state store (for handlers that need status).
func (u *TenantUpdater) StateStore() tenant.MigrationStateStore {
	return u.stateStore
}

// StartUpdate initiates a background schema migration for a tenant.
// Returns immediately after marking the tenant as "updating".
// The actual migration runs in a background goroutine.
//
// Errors:
//   - ErrTenantNotFound if tenant does not exist
//   - ErrAlreadyUpdating if migration is already in progress
//   - status != active is rejected (cannot update suspended/deleted tenants)
func (u *TenantUpdater) StartUpdate(ctx context.Context, tenantID string) error {
	// Prevent duplicate updates
	if _, loaded := u.running.LoadOrStore(tenantID, true); loaded {
		return fmt.Errorf("tenant %s is already being updated", tenantID)
	}

	// Validate tenant exists and is active
	t, err := u.registry.GetByID(ctx, tenantID)
	if err != nil {
		u.running.Delete(tenantID)
		return err
	}

	if t.Status != tenant.StatusActive {
		u.running.Delete(tenantID)
		return fmt.Errorf("cannot update tenant with status %q (must be active)", t.Status)
	}

	// Already up to date?
	if version.CompatibleSchema(t.SchemaVersion) {
		u.running.Delete(tenantID)
		return fmt.Errorf("tenant %s schema is already up to date (v%d)", t.Slug, t.SchemaVersion)
	}

	// Mark as updating in meta-database (blocks new HTTP requests via middleware)
	if err := u.registry.UpdateStatusByID(ctx, tenantID, tenant.StatusUpdating); err != nil {
		u.running.Delete(tenantID)
		return fmt.Errorf("set updating status: %w", err)
	}

	// Evict the connection pool so goose can acquire exclusive DDL locks
	u.manager.EvictPool(tenantID)

	u.log.Info("starting background schema update",
		"tenant_id", tenantID,
		"slug", t.Slug,
		"current_schema", t.SchemaVersion,
		"target_schema", version.ExpectedSchemaVersion,
	)

	// Run in background goroutine
	cfg := u.manager.Config()
	dsn := t.DSN(cfg.DBUser, cfg.DBPassword)

	u.wg.Go(func() {
		u.runMigrationBackground(tenantID, t.Slug, dsn)
	})

	return nil
}

// RetryUpdate re-runs goose up for a tenant in migration_failed status.
// goose up is idempotent — skips already applied migrations.
func (u *TenantUpdater) RetryUpdate(ctx context.Context, tenantID string) error {
	// Prevent duplicate updates
	if _, loaded := u.running.LoadOrStore(tenantID, true); loaded {
		return fmt.Errorf("tenant %s is already being updated", tenantID)
	}

	t, err := u.registry.GetByID(ctx, tenantID)
	if err != nil {
		u.running.Delete(tenantID)
		return err
	}

	if t.Status != tenant.StatusMigrationFailed {
		u.running.Delete(tenantID)
		return fmt.Errorf("cannot retry: tenant status is %q (must be migration_failed)", t.Status)
	}

	// Transition back to updating
	if err := u.registry.UpdateStatusByID(ctx, tenantID, tenant.StatusUpdating); err != nil {
		u.running.Delete(tenantID)
		return fmt.Errorf("set updating status: %w", err)
	}

	u.manager.EvictPool(tenantID)

	u.log.Info("retrying schema migration",
		"tenant_id", tenantID,
		"slug", t.Slug,
	)

	cfg := u.manager.Config()
	dsn := t.DSN(cfg.DBUser, cfg.DBPassword)

	u.wg.Go(func() {
		// Re-use same migration flow — goose up skips already applied.
		// pre_update_versions are already saved from the first attempt.
		u.runMigrationBackground(tenantID, t.Slug, dsn)
	})

	return nil
}

// RollbackUpdate rolls back migrations to pre_update_versions via goose down-to.
// Available only for tenants in migration_failed status.
func (u *TenantUpdater) RollbackUpdate(ctx context.Context, tenantID string) error {
	if _, loaded := u.running.LoadOrStore(tenantID, true); loaded {
		return fmt.Errorf("tenant %s is already being updated", tenantID)
	}

	t, err := u.registry.GetByID(ctx, tenantID)
	if err != nil {
		u.running.Delete(tenantID)
		return err
	}

	if t.Status != tenant.StatusMigrationFailed {
		u.running.Delete(tenantID)
		return fmt.Errorf("cannot rollback: tenant status is %q (must be migration_failed)", t.Status)
	}

	// Load saved versions
	versions, err := u.stateStore.GetPreUpdateVersions(ctx, tenantID)
	if err != nil {
		u.running.Delete(tenantID)
		return fmt.Errorf("get pre_update_versions: %w", err)
	}
	if versions == nil {
		u.running.Delete(tenantID)
		return fmt.Errorf("no pre_update_versions found for tenant %s", tenantID)
	}

	// Transition to updating (blocks business requests during rollback)
	if err := u.registry.UpdateStatusByID(ctx, tenantID, tenant.StatusUpdating); err != nil {
		u.running.Delete(tenantID)
		return fmt.Errorf("set updating status: %w", err)
	}

	u.manager.EvictPool(tenantID)

	u.log.Info("starting rollback",
		"tenant_id", tenantID,
		"slug", t.Slug,
		"target_versions", versions,
	)

	cfg := u.manager.Config()
	dsn := t.DSN(cfg.DBUser, cfg.DBPassword)

	u.wg.Go(func() {
		u.runRollbackBackground(tenantID, t.Slug, dsn, versions)
	})

	return nil
}

// WaitForAll blocks until all background migrations complete.
// Called during graceful shutdown.
func (u *TenantUpdater) WaitForAll() {
	u.cancel()
	u.wg.Wait()
}

// runMigrationBackground executes goose up and handles success/failure transitions.
func (u *TenantUpdater) runMigrationBackground(tenantID, slug, dsn string) {
	defer u.running.Delete(tenantID)

	ctx := u.ctx

	// 1. Snapshot current versions BEFORE migration (only if not already saved — retry case).
	existing, _ := u.stateStore.GetPreUpdateVersions(ctx, tenantID)
	if existing == nil {
		versions, verr := GetCurrentVersions(dsn)
		if verr != nil {
			u.log.Error("failed to snapshot goose versions",
				"tenant_id", tenantID,
				"error", verr,
			)
			u.transitionToFailed(ctx, tenantID, slug, fmt.Sprintf("snapshot versions: %v", verr))
			return
		}
		if serr := u.stateStore.SavePreUpdateVersions(ctx, tenantID, versions); serr != nil {
			u.log.Error("failed to persist pre_update_versions",
				"tenant_id", tenantID,
				"error", serr,
			)
			u.transitionToFailed(ctx, tenantID, slug, fmt.Sprintf("save versions: %v", serr))
			return
		}
	}

	// 2. Run migrations
	output, err := RunAll(dsn)
	if err != nil {
		u.log.Error("schema migration failed",
			"tenant_id", tenantID,
			"slug", slug,
			"error", err,
			"output", output,
		)
		u.transitionToFailed(ctx, tenantID, slug, fmt.Sprintf("%v\n%s", err, output))
		return
	}

	// 3. Success: update schema version, clear state, restore active.
	if serr := u.registry.UpdateSchemaVersion(ctx, tenantID, version.ExpectedSchemaVersion); serr != nil {
		u.log.Error("failed to update schema version after successful migration",
			"tenant_id", tenantID,
			"error", serr,
		)
	}

	if serr := u.stateStore.ClearState(ctx, tenantID); serr != nil {
		u.log.Error("failed to clear migration state",
			"tenant_id", tenantID,
			"error", serr,
		)
	}

	if serr := u.registry.UpdateStatusByID(ctx, tenantID, tenant.StatusActive); serr != nil {
		u.log.Error("CRITICAL: failed to restore tenant status after successful migration",
			"tenant_id", tenantID,
			"error", serr,
		)
	}

	u.log.Info("schema migration completed successfully",
		"tenant_id", tenantID,
		"slug", slug,
		"new_schema", version.ExpectedSchemaVersion,
		"output", output,
	)
}

// runRollbackBackground executes goose down-to and restores the tenant.
func (u *TenantUpdater) runRollbackBackground(tenantID, slug, dsn string, targetVersions map[string]int64) {
	defer u.running.Delete(tenantID)

	ctx := u.ctx

	output, err := RunDownTo(dsn, targetVersions)
	if err != nil {
		u.log.Error("rollback failed",
			"tenant_id", tenantID,
			"slug", slug,
			"error", err,
			"output", output,
		)
		u.transitionToFailed(ctx, tenantID, slug, fmt.Sprintf("rollback: %v\n%s", err, output))
		return
	}

	// Success: clear migration state, restore active.
	if serr := u.stateStore.ClearState(ctx, tenantID); serr != nil {
		u.log.Error("failed to clear migration state after rollback",
			"tenant_id", tenantID,
			"error", serr,
		)
	}

	if serr := u.registry.UpdateStatusByID(ctx, tenantID, tenant.StatusActive); serr != nil {
		u.log.Error("CRITICAL: failed to restore tenant status after rollback",
			"tenant_id", tenantID,
			"error", serr,
		)
	}

	u.log.Info("rollback completed successfully",
		"tenant_id", tenantID,
		"slug", slug,
		"output", output,
	)
}

// transitionToFailed sets the tenant to migration_failed and saves the error.
func (u *TenantUpdater) transitionToFailed(ctx context.Context, tenantID, slug, errMsg string) {
	if serr := u.stateStore.SaveLastError(ctx, tenantID, errMsg); serr != nil {
		u.log.Error("failed to save migration error",
			"tenant_id", tenantID,
			"error", serr,
		)
	}

	if serr := u.registry.UpdateStatusByID(ctx, tenantID, tenant.StatusMigrationFailed); serr != nil {
		u.log.Error("CRITICAL: failed to set migration_failed status",
			"tenant_id", tenantID,
			"slug", slug,
			"error", serr,
		)
	}
}
