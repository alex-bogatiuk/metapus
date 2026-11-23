package middleware

import (
	"github.com/alex-bogatiuk/metapus/internal/application/readaccess"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/domain/security_profile"
	"github.com/gin-gonic/gin"
)

// SecurityContext uses the shared application builder for HTTP and scheduled reads.
func SecurityContext(provider security_profile.ProfileProvider, resolvers ...security_profile.DimensionResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		if appctx.GetUser(c.Request.Context()) == nil {
			c.Next()
			return
		}
		ctx, err := readaccess.BuildSecurityContext(c.Request.Context(), provider, resolvers...)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
