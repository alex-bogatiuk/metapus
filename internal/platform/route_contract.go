package platform

import "github.com/gin-gonic/gin"

// RouteRegistration is a generic interface for route groups that don't follow
// the standard CRUD pattern (registers, reports). Each registration builds its
// handler and registers its own routes on the provided gin.RouterGroup.
type RouteRegistration interface {
	// RoutePrefix returns the URL path segment, e.g. "stock".
	RoutePrefix() string
	// RegisterRoutes builds the handler and mounts routes on the group.
	// The cfg parameter provides access to shared services (tenant manager, etc.).
	RegisterRoutes(group *gin.RouterGroup, cfg interface{})
}
