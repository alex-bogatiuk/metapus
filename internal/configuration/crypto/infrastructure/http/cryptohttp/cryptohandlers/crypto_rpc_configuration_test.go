package cryptohandlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/http/v1/middleware"
)

func TestRPCConfigurationHTTPPermissionAndStepUpBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, method string
		permissions  []string
		want         int
	}{
		{"missing catalog read", http.MethodGet, []string{"crypto_network:read"}, 403},
		{"missing network read", http.MethodGet, []string{"catalog:crypto_rpc_endpoint:read"}, 403},
		{"read does not grant update", http.MethodPatch, []string{"crypto_network:read", "catalog:crypto_rpc_endpoint:read"}, 403},
		{"policy update does not grant RPC update", http.MethodPatch, []string{"crypto_network:read", "crypto_network:update", "catalog:crypto_rpc_endpoint:read"}, 403},
		{"RPC update reaches sensitive action guard", http.MethodPatch, []string{"crypto_network:read", "catalog:crypto_rpc_endpoint:read", "catalog:crypto_rpc_endpoint:update"}, 428},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
				c.Request = c.Request.WithContext(appctx.WithUser(c.Request.Context(), &appctx.UserContext{UserID: id.New().String(), Permissions: tt.permissions}))
				permissions := map[string]struct{}{}
				for _, p := range tt.permissions {
					permissions[p] = struct{}{}
				}
				c.Set("permissions_set", permissions)
				c.Next()
			})
			NewCryptoRPCConfigurationHandler(nil).RegisterRoutes(router.Group(""), func(c *gin.Context) { c.AbortWithStatus(428) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tt.method, "/admin/crypto/networks/"+id.New().String()+"/rpc-configuration", nil))
			require.Equal(t, tt.want, response.Code, response.Body.String())
		})
	}
}
