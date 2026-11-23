//go:build integration

package platformcontent

import (
	"context"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/middleware"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/auth_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/migration"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/testsupport"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExchangeRateRoutesWithPersistedReaderPermissions(t *testing.T) {
	dsn := testsupport.ExchangeRateDSN(t)
	require.NotEmpty(t, dsn)
	output, err := migration.RunAll(dsn)
	require.NoError(t, err, output)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManagerFromRawPool(pool))
	_, err = pool.Exec(ctx, `INSERT INTO cat_currencies(code,name,iso_code,symbol) VALUES('USD','US Dollar','USD','$') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	userID := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO auth_users(id,email,password_hash,first_name,last_name,is_active,is_admin) VALUES($1,$2,'integration-no-login','Rate','Reader',true,false)`, userID, userID.String()+"@example.test")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO auth_user_roles(user_id,role_id) SELECT $1,id FROM auth_roles WHERE code='user'`, userID)
	require.NoError(t, err)
	users := auth_repo.NewUserRepo()
	stored, err := users.GetByID(ctx, userID)
	require.NoError(t, err)
	require.False(t, stored.IsAdmin)
	permissions, err := users.LoadPermissions(ctx, userID)
	require.NoError(t, err)
	require.Contains(t, permissions, "register:exchange_rate:read")
	require.NotContains(t, permissions, "catalog:rate_source:update")
	for _, role := range []string{"accountant", "manager", "admin"} {
		var granted bool
		require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM auth_roles r JOIN auth_role_permissions rp ON rp.role_id=r.id JOIN auth_permissions p ON p.id=rp.permission_id WHERE r.code=$1 AND p.code='register:exchange_rate:read')`, role).Scan(&granted))
		require.True(t, granted, role)
	}
	makeRouter := func(grants []string) *gin.Engine {
		router := gin.New()
		router.Use(middleware.ErrorHandler())
		router.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(appctx.WithUser(ctx, &appctx.UserContext{UserID: userID.String(), IsAdmin: stored.IsAdmin, Permissions: grants}))
			set := map[string]struct{}{}
			for _, p := range grants {
				set[p] = struct{}{}
			}
			c.Set("permissions_set", set)
			c.Next()
		})
		(&ExchangeRateRegisterRegistration{}).RegisterRoutes(router.Group("/api/v1/registers/exchange-rates"), v1.RouterConfig{})
		(&RateSourceRegistration{}).RegisterSubresourceRoutes(router.Group("/api/v1/catalog/rate-sources"), v1.RouterConfig{})
		return router
	}
	check := func(router *gin.Engine, method, path string, want int) {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		require.Equal(t, want, response.Code, response.Body.String())
	}
	reader := makeRouter(permissions)
	check(reader, http.MethodGet, "/api/v1/registers/exchange-rates/current", 200)
	check(reader, http.MethodGet, "/api/v1/registers/exchange-rates/history", 400)
	check(reader, http.MethodGet, "/api/v1/registers/exchange-rates/current?limit=101", 400)
	check(reader, http.MethodGet, "/api/v1/catalog/rate-sources/"+id.New().String()+"/mappings", 403)
	check(reader, http.MethodPut, "/api/v1/catalog/rate-sources/"+id.New().String()+"/mappings/"+id.New().String()+"/"+id.New().String(), 403)
	check(makeRouter(nil), http.MethodGet, "/api/v1/registers/exchange-rates/current", 403)
}
