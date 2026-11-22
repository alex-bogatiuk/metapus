package cryptohandlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"metapus/pkg/sdk/appctx"
	"metapus/pkg/sdk/http/middleware"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/integrations"
	"metapus/pkg/sdk/json"
)

type rpcKeyNamesRepo struct {
	query rpcconfig.RPCKeyQuery
}

func (r *rpcKeyNamesRepo) RPCKeyOptions(_ context.Context, q rpcconfig.RPCKeyQuery) (rpcconfig.RPCKeyOptions, error) {
	r.query = q
	return rpcconfig.RPCKeyOptions{Required: "sepolia", Names: []string{"sepolia"}, Accounts: []integrations.AccountKeys{{ID: "a", Name: "Infura", HasCredentials: true, Keys: []string{"sepolia"}}}}, nil
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
			NewRPCKeyNamesHandler(repo).RegisterRoutes(router.Group("/system"))
			target := "/system/integrations/pools/" + pool.String() + "/rpc-key-names?role=rpc&url=https%3A%2F%2Fsepolia.infura.io%2Fv3%2F"
			if tc.networkID != "" {
				target += "&networkId=" + tc.networkID
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
			require.Equal(t, tc.status, response.Code, response.Body.String())
			if tc.status != http.StatusOK {
				return
			}
			var body cryptodto.IntegrationRPCKeyOptionsResponse
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
