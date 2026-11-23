package middleware

import (
	"context"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestProviderDecisionPermissionSystemAdminBoundary(t *testing.T) {
	permission := "document:crypto_provider_balance_operation:approve"
	for _, tc := range []struct {
		name               string
		user               appctx.UserContext
		permitted, allowed bool
	}{
		{name: "flag", user: appctx.UserContext{IsAdmin: true}, allowed: true},
		{name: "role", user: appctx.UserContext{Roles: []string{"admin"}}, allowed: true},
		{name: "security is not system admin", user: appctx.UserContext{Roles: []string{"security_admin"}}},
		{name: "explicit approve", permitted: true, allowed: true},
		{name: "create does not imply approve", user: appctx.UserContext{Permissions: []string{"document:crypto_provider_balance_operation:create"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			reached := false
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(appctx.WithUser(context.Background(), &tc.user))
				if tc.permitted {
					c.Set("permissions_set", map[string]struct{}{permission: {}})
				}
			})
			router.POST("/decision", RequirePermissionOrSystemAdmin(permission), func(c *gin.Context) { reached = true; c.Status(204) })
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/decision", nil))
			require.Equal(t, tc.allowed, reached)
		})
	}
}
