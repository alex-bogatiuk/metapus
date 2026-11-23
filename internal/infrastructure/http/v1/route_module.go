package v1

import (
	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/metadata"
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

	authenticated, protected gin.HandlersChain
}

// Authenticated returns a group at path under Root behind the platform's
// tenant resolution, authentication and admin guard: Protected's chain
// without the security profile, for routes that enforce a policy of their own
// (a merchant portal).
func (rc RouteContext) Authenticated(path string) *gin.RouterGroup {
	if len(rc.authenticated) == 0 {
		panic("v1.RouteContext.Authenticated: the context was not built by NewRouter")
	}
	return rc.Root.Group(path, rc.authenticated...)
}

// ProtectedGroup returns a group at path under Root behind the same chain as
// Protected: tenant resolution, authentication, admin guard, the security
// profile and the authorized reader.
func (rc RouteContext) ProtectedGroup(path string) *gin.RouterGroup {
	if len(rc.protected) == 0 {
		panic("v1.RouteContext.ProtectedGroup: the context was not built by NewRouter")
	}
	return rc.Root.Group(path, rc.protected...)
}
