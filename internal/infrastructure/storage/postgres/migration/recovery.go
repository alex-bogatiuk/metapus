package migration

import (
	"context"

	"metapus/internal/core/tenant"
	"metapus/pkg/logger"
)

// RecoverStuckTenants finds tenants stuck in "updating" status (from a previous
// server crash during migration) and transitions them to "migration_failed".
// This allows admins to decide whether to retry or rollback.
//
// Must be called during server startup, after meta-database is connected but
// before the HTTP server starts accepting requests.
func RecoverStuckTenants(ctx context.Context, registry tenant.Registry, log *logger.Logger) {
	tenants, err := registry.ListAll(ctx)
	if err != nil {
		log.Error("failed to list tenants for stuck recovery", "error", err)
		return
	}

	for _, t := range tenants {
		if t.Status == tenant.StatusUpdating {
			if serr := registry.UpdateStatusByID(ctx, t.ID, tenant.StatusMigrationFailed); serr != nil {
				log.Error("failed to recover stuck tenant",
					"tenant_id", t.ID,
					"slug", t.Slug,
					"error", serr,
				)
				continue
			}
			log.Warn("recovered stuck tenant: updating → migration_failed",
				"tenant_id", t.ID,
				"slug", t.Slug,
			)
		}
	}
}
