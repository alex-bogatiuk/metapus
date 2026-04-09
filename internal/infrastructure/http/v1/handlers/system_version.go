package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/version"
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
	c.JSON(http.StatusOK, gin.H{
		"version":               h.version,
		"buildTime":             h.buildTime,
		"expectedSchemaVersion": version.ExpectedSchemaVersion,
	})
}
