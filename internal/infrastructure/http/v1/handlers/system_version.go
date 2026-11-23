package handlers

import (
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/version"
)

// SystemVersionHandler exposes build and schema version information.
// Registered on public routes (no auth / no tenant required).
type SystemVersionHandler struct {
	version   string
	buildTime string
}

// NewSystemVersionHandler creates a handler that serves version metadata.
func NewSystemVersionHandler(ver, buildTime string) *SystemVersionHandler {
	return &SystemVersionHandler{
		version:   ver,
		buildTime: buildTime,
	}
}

// Version returns the current server binary version, build time,
// and expected schema version.
// GET /api/v1/system/version
func (h *SystemVersionHandler) Version(c *gin.Context) {
	c.JSON(http.StatusOK, dto.SystemVersionResponse{Version: h.version, BuildTime: h.buildTime, ExpectedSchemaVersion: version.ExpectedSchemaVersion})
}
