package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"metapus/internal/application/integrationapp"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
)

type rpcKeyNamesRepo struct {
	integrations.AdministrationRepository
	query integrations.RPCKeyQuery
}

func (r *rpcKeyNamesRepo) RPCKeyOptions(_ context.Context, q integrations.RPCKeyQuery) (integrations.RPCKeyOptions, error) {
	r.query = q
	return integrations.RPCKeyOptions{Required: "sepolia", Names: []string{"sepolia"}, Accounts: []integrations.RPCKeyAccount{{ID: "a", Name: "Infura", HasCredentials: true, Keys: []string{"sepolia"}}}}, nil
}

func TestRPCKeyNamesPassesEndpointDraftAndChecksPermissions(t *testing.T) {
	pool, network := id.New(), id.New()
	for _, tc := range []struct {
		name, permission, networkID string
		status                      int
	}{
		{"unrelated permission", "catalog:currency:read", network.String(), http.StatusForbidden},
		{"endpoint editor", "catalog:crypto_rpc_endpoint:update", network.String(), http.StatusOK},
		{"pool reader", "catalog:integration_pool:read", "", http.StatusOK},
		{"invalid network", "catalog:integration_pool:read", "not-an-id", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &rpcKeyNamesRepo{}
			router := gin.New()
			router.Use(middleware.ErrorHandler())
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(appctx.WithUser(c.Request.Context(), &appctx.UserContext{Permissions: []string{tc.permission}}))
				c.Set("permissions_set", map[string]struct{}{tc.permission: {}})
				c.Next()
			})
			guardCalled := false
			NewIntegrationHandler(integrationapp.New(repo, nil, nil)).RegisterRoutes(router.Group("/system"), func(c *gin.Context) { guardCalled = true; c.Next() })
			target := "/system/integrations/pools/" + pool.String() + "/rpc-key-names?role=rpc&url=https%3A%2F%2Fsepolia.infura.io%2Fv3%2F"
			if tc.networkID != "" {
				target += "&networkId=" + tc.networkID
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.False(t, guardCalled, "reading key names must not require mutation step-up")
			if tc.status != http.StatusOK {
				return
			}
			var body dto.IntegrationRPCKeyOptionsResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, "sepolia", body.Required)
			require.Equal(t, []string{"sepolia"}, body.Accounts[0].Keys)
			require.Equal(t, pool, repo.query.PoolID)
			require.Equal(t, "rpc", repo.query.Role)
			require.Equal(t, "https://sepolia.infura.io/v3/", repo.query.URL)
			if tc.networkID != "" {
				require.Equal(t, network, repo.query.NetworkID)
			}
		})
	}
}
