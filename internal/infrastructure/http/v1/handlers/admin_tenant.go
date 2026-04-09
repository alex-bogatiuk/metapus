package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/tenant"
	"metapus/internal/core/version"
	"metapus/internal/infrastructure/storage/postgres/migration"
)

// AdminTenantHandler provides Cloud Control Plane endpoints
// for managing tenant version groups and viewing tenant status.
//
// These endpoints query the meta-database directly (not the tenant DB).
// They require admin role and are designed for cloud operators.
type AdminTenantHandler struct {
	base     *BaseHandler
	registry tenant.Registry
	updater  *migration.TenantUpdater
}

// NewAdminTenantHandler creates an admin handler for tenant management.
func NewAdminTenantHandler(base *BaseHandler, registry tenant.Registry, updater *migration.TenantUpdater) *AdminTenantHandler {
	return &AdminTenantHandler{base: base, registry: registry, updater: updater}
}

// TenantSummary is the response DTO for tenant list and details.
type TenantSummary struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	DisplayName   string `json:"displayName"`
	DBName        string `json:"dbName"`
	Status        string `json:"status"`
	Plan          string `json:"plan"`
	SchemaVersion int    `json:"schemaVersion"`
	VersionGroup  string `json:"versionGroup"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	// Computed
	SchemaUpToDate bool `json:"schemaUpToDate"`
}

func toTenantSummary(t *tenant.Tenant) TenantSummary {
	return TenantSummary{
		ID:             t.ID,
		Slug:           t.Slug,
		DisplayName:    t.DisplayName,
		DBName:         t.DBName,
		Status:         string(t.Status),
		Plan:           string(t.Plan),
		SchemaVersion:  t.SchemaVersion,
		VersionGroup:   t.VersionGroup,
		CreatedAt:      t.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:      t.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		SchemaUpToDate: version.CompatibleSchema(t.SchemaVersion),
	}
}

// List returns all tenants with their version and schema information.
// GET /api/v1/admin/tenants
func (h *AdminTenantHandler) List(c *gin.Context) {
	tenants, err := h.registry.ListAll(c.Request.Context())
	if err != nil {
		h.base.HandleError(c, err)
		return
	}

	items := make([]TenantSummary, 0, len(tenants))
	for _, t := range tenants {
		items = append(items, toTenantSummary(t))
	}

	// Compute summary stats
	var activeCount, outdatedCount int
	groups := map[string]int{}
	for _, t := range tenants {
		if t.IsActive() {
			activeCount++
		}
		if !version.CompatibleSchema(t.SchemaVersion) {
			outdatedCount++
		}
		vg := t.VersionGroup
		if vg == "" {
			vg = "(default)"
		}
		groups[vg]++
	}

	c.JSON(http.StatusOK, gin.H{
		"items":            items,
		"total":            len(items),
		"activeCount":      activeCount,
		"outdatedCount":    outdatedCount,
		"versionGroups":    groups,
		"expectedSchema":   version.ExpectedSchemaVersion,
		"serverVersion":    c.GetString("_server_version"), // set by middleware or ignored
	})
}

// Get returns a single tenant by ID.
// GET /api/v1/admin/tenants/:tenantId
func (h *AdminTenantHandler) Get(c *gin.Context) {
	tenantID := c.Param("tenantId")

	t, err := h.registry.GetByID(c.Request.Context(), tenantID)
	if err != nil {
		h.base.HandleError(c, err)
		return
	}

	c.JSON(http.StatusOK, toTenantSummary(t))
}

// PromoteRequest is the request body for version group assignment.
type PromoteRequest struct {
	VersionGroup string `json:"versionGroup" binding:"required"`
}

// Promote assigns a tenant to a version group.
// PUT /api/v1/admin/tenants/:tenantId/version-group
func (h *AdminTenantHandler) Promote(c *gin.Context) {
	tenantID := c.Param("tenantId")

	var req PromoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "versionGroup is required"})
		return
	}

	// Verify tenant exists
	t, err := h.registry.GetByID(c.Request.Context(), tenantID)
	if err != nil {
		h.base.HandleError(c, err)
		return
	}

	oldGroup := t.VersionGroup

	if err := h.registry.UpdateVersionGroup(c.Request.Context(), tenantID, req.VersionGroup); err != nil {
		h.base.HandleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "tenant promoted",
		"tenantId":  tenantID,
		"slug":      t.Slug,
		"oldGroup":  oldGroup,
		"newGroup":  req.VersionGroup,
	})
}

// UpdateSchemaVersionRequest is the request body for schema version update.
type UpdateSchemaVersionRequest struct {
	SchemaVersion int `json:"schemaVersion" binding:"required"`
}

// UpdateSchemaVersion sets the schema version for a tenant.
// PUT /api/v1/admin/tenants/:tenantId/schema-version
func (h *AdminTenantHandler) UpdateSchemaVersion(c *gin.Context) {
	tenantID := c.Param("tenantId")

	var req UpdateSchemaVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schemaVersion is required"})
		return
	}

	if req.SchemaVersion < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schemaVersion must be >= 0"})
		return
	}

	if err := h.registry.UpdateSchemaVersion(c.Request.Context(), tenantID, req.SchemaVersion); err != nil {
		h.base.HandleError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "schema version updated",
		"tenantId":      tenantID,
		"schemaVersion": req.SchemaVersion,
		"upToDate":      version.CompatibleSchema(req.SchemaVersion),
	})
}

// Stats returns aggregate statistics for the control plane dashboard.
// GET /api/v1/admin/tenants/stats
func (h *AdminTenantHandler) Stats(c *gin.Context) {
	tenants, err := h.registry.ListAll(c.Request.Context())
	if err != nil {
		h.base.HandleError(c, err)
		return
	}

	var active, suspended, outdated int
	groups := map[string]int{}
	schemaVersions := map[int]int{}

	for _, t := range tenants {
		switch t.Status {
		case tenant.StatusActive:
			active++
		case tenant.StatusSuspended:
			suspended++
		}

		if !version.CompatibleSchema(t.SchemaVersion) {
			outdated++
		}

		vg := t.VersionGroup
		if vg == "" {
			vg = "(default)"
		}
		groups[vg]++
		schemaVersions[t.SchemaVersion]++
	}

	c.JSON(http.StatusOK, gin.H{
		"totalTenants":          len(tenants),
		"activeTenants":         active,
		"suspendedTenants":      suspended,
		"outdatedSchemas":       outdated,
		"expectedSchemaVersion": version.ExpectedSchemaVersion,
		"versionGroups":         groups,
		"schemaVersions":        schemaVersions,
	})
}

// TriggerUpdate starts a background schema migration for a tenant.
// POST /api/v1/admin/tenants/:tenantId/update
func (h *AdminTenantHandler) TriggerUpdate(c *gin.Context) {
	tenantID := c.Param("tenantId")

	if err := h.updater.StartUpdate(c.Request.Context(), tenantID); err != nil {
		h.base.HandleError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "schema update started",
		"tenantId": tenantID,
		"status":   "updating",
	})
}

// RetryUpdate re-runs schema migration for a tenant in migration_failed status.
// POST /api/v1/admin/tenants/:tenantId/retry-update
func (h *AdminTenantHandler) RetryUpdate(c *gin.Context) {
	tenantID := c.Param("tenantId")

	if err := h.updater.RetryUpdate(c.Request.Context(), tenantID); err != nil {
		h.base.HandleError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "migration retry started",
		"tenantId": tenantID,
		"status":   "updating",
	})
}

// RollbackUpdate rolls back migrations to pre-update state.
// POST /api/v1/admin/tenants/:tenantId/rollback-update
func (h *AdminTenantHandler) RollbackUpdate(c *gin.Context) {
	tenantID := c.Param("tenantId")

	if err := h.updater.RollbackUpdate(c.Request.Context(), tenantID); err != nil {
		h.base.HandleError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "rollback started",
		"tenantId": tenantID,
		"status":   "updating",
	})
}

// MigrationStatus returns current migration state for a tenant.
// GET /api/v1/admin/tenants/:tenantId/migration-status
func (h *AdminTenantHandler) MigrationStatus(c *gin.Context) {
	tenantID := c.Param("tenantId")

	state, err := h.updater.StateStore().GetState(c.Request.Context(), tenantID)
	if err != nil {
		h.base.HandleError(c, err)
		return
	}

	t, terr := h.registry.GetByID(c.Request.Context(), tenantID)
	if terr != nil {
		h.base.HandleError(c, terr)
		return
	}

	resp := gin.H{
		"tenantId": tenantID,
		"status":   string(t.Status),
	}

	if state != nil {
		resp["preUpdateVersions"] = state.PreUpdateVersions
		resp["lastError"] = state.LastError
		resp["updatedAt"] = state.UpdatedAt
	}

	c.JSON(http.StatusOK, resp)
}

// --- Internal endpoints (for Updater Agent, no auth) ---
// These use :id param instead of :tenantId (different route group).

// InternalTriggerUpdate is the internal (no-auth) variant of TriggerUpdate.
// POST /internal/tenants/:id/trigger-update
func (h *AdminTenantHandler) InternalTriggerUpdate(c *gin.Context) {
	tenantID := c.Param("id")

	if err := h.updater.StartUpdate(c.Request.Context(), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "schema update started",
		"tenantId": tenantID,
		"status":   "updating",
	})
}

// InternalRetryUpdate is the internal variant of RetryUpdate.
// POST /internal/tenants/:id/retry-update
func (h *AdminTenantHandler) InternalRetryUpdate(c *gin.Context) {
	tenantID := c.Param("id")

	if err := h.updater.RetryUpdate(c.Request.Context(), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "migration retry started",
		"tenantId": tenantID,
		"status":   "updating",
	})
}

// InternalRollbackUpdate is the internal variant of RollbackUpdate.
// POST /internal/tenants/:id/rollback-update
func (h *AdminTenantHandler) InternalRollbackUpdate(c *gin.Context) {
	tenantID := c.Param("id")

	if err := h.updater.RollbackUpdate(c.Request.Context(), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "rollback started",
		"tenantId": tenantID,
		"status":   "updating",
	})
}

// InternalMigrationStatus is the internal variant of MigrationStatus.
// GET /internal/tenants/:id/migration-status
func (h *AdminTenantHandler) InternalMigrationStatus(c *gin.Context) {
	tenantID := c.Param("id")

	state, err := h.updater.StateStore().GetState(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	t, terr := h.registry.GetByID(c.Request.Context(), tenantID)
	if terr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": terr.Error()})
		return
	}

	resp := gin.H{
		"tenantId": tenantID,
		"status":   string(t.Status),
	}

	if state != nil {
		resp["preUpdateVersions"] = state.PreUpdateVersions
		resp["lastError"] = state.LastError
		resp["updatedAt"] = state.UpdatedAt
	}

	c.JSON(http.StatusOK, resp)
}

