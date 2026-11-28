package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/core/version"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
)

// AdminTenantHandler exposes read-only tenant metadata for administrators.
// Mutating tenant schemas is intentionally restricted to the deployment CLI.
type AdminTenantHandler struct {
	base                 *BaseHandler
	registry             tenant.Registry
	expected             tenant.Schema
	controlPlaneTenantID string
}

// NewAdminTenantHandler creates a read-only tenant monitoring handler.
// expected is the schema of a tenant database migrated by this binary's
// configuration; the zero value compares the platform's version only.
func NewAdminTenantHandler(base *BaseHandler, registry tenant.Registry, expected tenant.Schema) *AdminTenantHandler {
	return &AdminTenantHandler{base: base, registry: registry, expected: expected}
}

// SetControlPlaneTenantID configures the optional platform-ops tenant allowed
// to enumerate all tenants in the deployment.
func (h *AdminTenantHandler) SetControlPlaneTenantID(tenantID string) {
	h.controlPlaneTenantID = tenantID
}

func (h *AdminTenantHandler) isControlPlaneCaller(c *gin.Context) bool {
	callerTenantID := tenant.GetTenantID(c.Request.Context())
	return h.controlPlaneTenantID != "" && callerTenantID == h.controlPlaneTenantID
}

// upToDate reports whether t's database is at the expected schema.
func (h *AdminTenantHandler) upToDate(t *tenant.Tenant) bool {
	if h.expected.Configuration == "" {
		return version.CompatibleSchema(t.SchemaVersion)
	}
	return t.Schema().Matches(h.expected)
}

func (h *AdminTenantHandler) toTenantSummary(t *tenant.Tenant) dto.TenantSummary {
	return dto.TenantSummary{
		ID:             t.ID,
		Slug:           t.Slug,
		DisplayName:    t.DisplayName,
		DBName:         t.DBName,
		Status:         string(t.Status),
		Plan:           string(t.Plan),
		SchemaVersion:  t.SchemaVersion,
		CreatedAt:      t.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:      t.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		SchemaUpToDate: h.upToDate(t),
	}
}

// List returns tenant metadata: all tenants for the control-plane tenant, or
// strictly the caller's own bound tenant for regular tenant administrators.
func (h *AdminTenantHandler) List(c *gin.Context) {
	var tenants []*tenant.Tenant
	if h.isControlPlaneCaller(c) {
		all, err := h.registry.ListAll(c.Request.Context())
		if err != nil {
			h.base.HandleError(c, err)
			return
		}
		tenants = all
	} else {
		callerTenantID := tenant.GetTenantID(c.Request.Context())
		if callerTenantID == "" {
			h.base.HandleError(c, apperror.NewForbidden("tenant context is required"))
			return
		}
		own, err := h.registry.GetByID(c.Request.Context(), callerTenantID)
		if err != nil {
			h.base.HandleError(c, err)
			return
		}
		tenants = []*tenant.Tenant{own}
	}

	items := make([]dto.TenantSummary, 0, len(tenants))
	activeCount := 0
	outdatedCount := 0
	for _, t := range tenants {
		items = append(items, h.toTenantSummary(t))
		if t.IsActive() {
			activeCount++
		}
		if !h.upToDate(t) {
			outdatedCount++
		}
	}

	c.JSON(http.StatusOK, dto.TenantListResponse{Items: items, Total: len(items), ActiveCount: activeCount, OutdatedCount: outdatedCount, ExpectedSchema: version.ExpectedSchemaVersion})
}

// Get returns a single tenant by ID.
func (h *AdminTenantHandler) Get(c *gin.Context) {
	targetID := c.Param("tenantId")
	if !h.isControlPlaneCaller(c) && targetID != tenant.GetTenantID(c.Request.Context()) {
		h.base.HandleError(c, apperror.NewForbidden("cross-tenant metadata access is forbidden"))
		return
	}
	t, err := h.registry.GetByID(c.Request.Context(), targetID)
	if err != nil {
		h.base.HandleError(c, err)
		return
	}

	c.JSON(http.StatusOK, h.toTenantSummary(t))
}
