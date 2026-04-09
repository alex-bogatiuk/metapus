package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"metapus/internal/core/tenant"
)

// TenantRouteHandler provides an internal endpoint for the reverse proxy
// to determine which version-group a tenant belongs to.
//
// Used by Nginx auth_request: the proxy sends a subrequest with X-Tenant-ID,
// this handler returns 200 + X-Version-Group header. Nginx then routes
// to the correct upstream based on the header value.
//
// This endpoint is NOT exposed to public traffic — it lives on /internal/.
type TenantRouteHandler struct {
	registry tenant.Registry
}

// NewTenantRouteHandler creates a handler for tenant routing lookups.
func NewTenantRouteHandler(registry tenant.Registry) *TenantRouteHandler {
	return &TenantRouteHandler{registry: registry}
}

// Route resolves a tenant ID to its version group.
//
//	GET /internal/route
//	Header: X-Tenant-ID: <uuid>
//	Response 200: X-Version-Group header set
//	Response 404: tenant not found
//	Response 403: tenant not active
func (h *TenantRouteHandler) Route(c *gin.Context) {
	rawID := c.GetHeader("X-Tenant-ID")
	if rawID == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	if _, err := uuid.Parse(rawID); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	t, err := h.registry.GetByID(c.Request.Context(), rawID)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	if !t.IsActive() {
		c.Status(http.StatusForbidden)
		return
	}

	// Set the version group header for Nginx to use in proxy_pass
	vg := t.VersionGroup
	if vg == "" {
		vg = "default"
	}
	c.Header("X-Version-Group", vg)
	c.Header("X-Tenant-DB", t.DBName)
	c.Status(http.StatusOK)
}
