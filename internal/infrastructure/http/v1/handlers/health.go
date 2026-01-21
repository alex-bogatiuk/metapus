// Package handlers provides HTTP request handlers.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
)

// HealthHandler provides health check endpoints.
type HealthHandler struct {
	pool *postgres.Pool
}

// NewHealthHandler creates a new health handler.
func NewHealthHandler(pool *postgres.Pool) *HealthHandler {
	return &HealthHandler{pool: pool}
}

// Live handles liveness probe (is the process alive?).
// GET /health/live
func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

// Ready handles readiness probe (is the service ready to accept traffic?).
// GET /health/ready
func (h *HealthHandler) Ready(c *gin.Context) {
	// Check database connection
	if err := h.pool.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "error",
			"checks": map[string]string{
				"database": "unhealthy: " + err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"checks": map[string]string{
			"database": "healthy",
		},
	})
}

// Info returns application information.
// GET /health/info
func (h *HealthHandler) Info(c *gin.Context) {
	stat := h.pool.Stat()

	c.JSON(http.StatusOK, gin.H{
		"app":     "metapus",
		"version": "0.1.0",
		"database": map[string]any{
			"total_conns":    stat.TotalConns(),
			"acquired_conns": stat.AcquiredConns(),
			"idle_conns":     stat.IdleConns(),
			"max_conns":      stat.MaxConns(),
		},
	})
}

// --- Multi-Tenant Health Handler ---

// MultiTenantHealthHandler provides health check endpoints for multi-tenant architecture.
type MultiTenantHealthHandler struct {
	metaPool      *pgxpool.Pool
	tenantManager *tenant.Manager
}

// NewHealthHandlerMultiTenant creates a health handler for multi-tenant setup.
func NewHealthHandlerMultiTenant(metaPool *pgxpool.Pool, tenantManager *tenant.Manager) *MultiTenantHealthHandler {
	return &MultiTenantHealthHandler{
		metaPool:      metaPool,
		tenantManager: tenantManager,
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
				"meta_database": "unhealthy: " + err.Error(),
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

	c.JSON(http.StatusOK, gin.H{
		"app":     "metapus",
		"version": "0.1.0",
		"mode":    "multi-tenant",
		"meta_database": map[string]any{
			"total_conns":    metaStat.TotalConns(),
			"acquired_conns": metaStat.AcquiredConns(),
			"idle_conns":     metaStat.IdleConns(),
		},
		"tenants": map[string]any{
			"active_pools":  tenantStats.TotalPools,
			"total_conns":   tenantStats.TotalConns,
			"idle_conns":    tenantStats.IdleConns,
			"acquired_conn": tenantStats.AcquiredConns,
		},
	})
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
