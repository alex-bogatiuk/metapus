package v1

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/metadata"
)

// RouteModule mounts one configuration's routes on the platform router.
// Modules run after the platform routes are registered, so Metadata is complete.
type RouteModule interface {
	RegisterRoutes(rc RouteContext)
}

// RouteContext exposes the platform router groups and shared dependencies.
type RouteContext struct {
	// Root mounts routes under their own prefixes (public pages, partner APIs).
	Root *gin.Engine
	// API is /api/v1 without tenant resolution or authentication.
	API *gin.RouterGroup
	// Protected is /api/v1 behind tenant, auth, admin guard and security context.
	Protected *gin.RouterGroup
	// System is /api/v1/system on Protected, restricted to the admin role.
	System *gin.RouterGroup
	// Config carries the platform dependencies the router was built with.
	Config RouterConfig
	// Metadata is the populated entity metadata registry.
	Metadata *metadata.Registry
}
