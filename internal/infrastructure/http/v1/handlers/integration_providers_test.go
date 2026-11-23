package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/middleware"
)

func TestIntegrationProvidersUsesExistingCatalogPermissions(t *testing.T) {
	for _, tc := range []struct {
		name       string
		user       bool
		permission string
		status     int
	}{
		{"anonymous", false, "", http.StatusUnauthorized},
		{"unrelated permission", true, "catalog:currency:read", http.StatusForbidden},
		{"account creator", true, "catalog:integration_account:create", http.StatusOK},
		{"pool reader", true, "catalog:integration_pool:read", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(middleware.ErrorHandler())
			router.Use(func(c *gin.Context) {
				if tc.user {
					c.Request = c.Request.WithContext(appctx.WithUser(c.Request.Context(), &appctx.UserContext{Permissions: []string{tc.permission}}))
					c.Set("permissions_set", map[string]struct{}{tc.permission: {}})
				}
				c.Next()
			})
			guardCalled := false
			NewIntegrationHandler(nil, integrations.PlatformCatalog()).RegisterRoutes(router.Group("/system"), func(c *gin.Context) { guardCalled = true; c.Next() })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/system/integrations/providers", nil))
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.False(t, guardCalled, "reading provider definitions must not require mutation step-up")
			if tc.status == http.StatusOK {
				var body dto.IntegrationProvidersResponse
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				require.Len(t, body.Kinds, len(integrations.PlatformKinds()))
				require.Equal(t, integrations.CoinGeckoDemo, body.Providers[0].ID, "the handler lists the catalog it was given")
			}
		})
	}
}
