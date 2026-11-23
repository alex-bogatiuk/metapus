package handlers

import (
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/core/version"
)

// AdminTenantHandler exposes read-only tenant metadata for administrators.
// Mutating tenant schemas is intentionally restricted to the deployment CLI.
type AdminTenantHandler struct {
	base     *BaseHandler
	registry tenant.Registry
	expected tenant.Schema
}

// NewAdminTenantHandler creates a read-only tenant monitoring handler.
// expected is the schema of a tenant database migrated by this binary's
// configuration; the zero value compares the platform's version only.
func NewAdminTenantHandler(base *BaseHandler, registry tenant.Registry, expected tenant.Schema) *AdminTenantHandler {
	return &AdminTenantHandler{base: base, registry: registry, expected: expected}
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

// List returns all tenants with schema compatibility information.
func (h *AdminTenantHandler) List(c *gin.Context) {
	tenants, err := h.registry.ListAll(c.Request.Context())
	if err != nil {
		h.base.HandleError(c, err)
		return
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
	t, err := h.registry.GetByID(c.Request.Context(), c.Param("tenantId"))
	if err != nil {
		h.base.HandleError(c, err)
		return
	}

	c.JSON(http.StatusOK, h.toTenantSummary(t))
}
