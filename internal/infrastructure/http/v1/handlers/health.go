// Package handlers provides HTTP request handlers.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb"
)

// --- Multi-Tenant Health Handler ---

// MultiTenantHealthHandler provides health check endpoints for multi-tenant architecture.
type MultiTenantHealthHandler struct {
	metaPool      *pgxpool.Pool
	tenantManager *tenantdb.Manager
	version       string
}

// NewHealthHandlerMultiTenant creates a health handler for multi-tenant setup.
func NewHealthHandlerMultiTenant(metaPool *pgxpool.Pool, tenantManager *tenantdb.Manager, version string) *MultiTenantHealthHandler {
	return &MultiTenantHealthHandler{
		metaPool:      metaPool,
		tenantManager: tenantManager,
		version:       version,
	}
}

// Live handles liveness probe.
// GET /health/live
func (h *MultiTenantHealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

// Ready handles readiness probe - checks meta-database connection.
// GET /health/ready
func (h *MultiTenantHealthHandler) Ready(c *gin.Context) {
	ctx := c.Request.Context()

	// Check meta-database connection
	if err := h.metaPool.Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "error",
			"checks": map[string]string{
				"meta_database": "unhealthy",
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"checks": map[string]string{
			"meta_database": "healthy",
		},
	})
}

// Info returns application information with multi-tenant stats.
// GET /health/info
func (h *MultiTenantHealthHandler) Info(c *gin.Context) {
	metaStat := h.metaPool.Stat()
	tenantStats := h.tenantManager.Stats()

	c.JSON(http.StatusOK, dto.HealthInfoResponse{App: "metapus", Version: h.version, Mode: "multi-tenant",
		MetaDatabase: &dto.PoolStatsResponse{TotalConns: metaStat.TotalConns(), AcquiredConns: metaStat.AcquiredConns(), IdleConns: metaStat.IdleConns()},
		Tenants:      &dto.TenantPoolStatsResponse{ActivePools: tenantStats.TotalPools, TotalConns: tenantStats.TotalConns, IdleConns: tenantStats.IdleConns, AcquiredConns: tenantStats.AcquiredConns}})
}

// TenantsStats returns detailed statistics for all tenant pools.
// GET /health/tenants
func (h *MultiTenantHealthHandler) TenantsStats(c *gin.Context) {
	stats := h.tenantManager.Stats()

	tenantDetails := make([]gin.H, 0, len(stats.Tenants))
	for _, t := range stats.Tenants {
		tenantDetails = append(tenantDetails, gin.H{
			"tenant_id":      t.TenantID,
			"db_name":        t.DBName,
			"total_conns":    t.TotalConns,
			"idle_conns":     t.IdleConns,
			"acquired_conns": t.AcquiredConns,
			"active_refs":    t.ActiveRefs,
			"last_used":      t.LastUsed,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total_pools": stats.TotalPools,
		"total_conns": stats.TotalConns,
		"tenants":     tenantDetails,
	})
}
