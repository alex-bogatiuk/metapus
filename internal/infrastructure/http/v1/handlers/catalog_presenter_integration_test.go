//go:build integration

package handlers_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"metapus/internal/application/readaccess"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/security"
	"metapus/internal/domain/catalogs/counterparty"
	"metapus/internal/domain/security_profile"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
	"metapus/internal/metadata"
)

func TestCatalogPresenterMasksEveryResponseAndReauthorizesReplay(t *testing.T) {
	tenants := testsupport.NewSecurityTenants(t)
	tenant := tenants[0]
	reg := metadata.NewRegistry()
	def := metadata.Inspect(counterparty.Counterparty{}, "Counterparty", metadata.TypeCatalog)
	def.Key, def.TableName, def.ReadPermission = "counterparty", "cat_counterparties", "counterparty:read"
	reg.Register(def)
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer engine.Stop()
	reads := postgres.NewAuthorizedReadService(reg, engine)
	svc := counterparty.NewService(catalog_repo.NewCounterpartyRepo(), nil)
	svc.SetPolicyEngine(engine)
	h := handlers.NewCatalogHandler(handlers.NewBaseHandler(), handlers.CatalogHandlerConfig[*counterparty.Counterparty, dto.CreateCounterpartyRequest, dto.UpdateCounterpartyRequest]{
		Service: svc.CatalogService, EntityName: "counterparty",
		MapCreateDTO: func(req dto.CreateCounterpartyRequest) *counterparty.Counterparty { return req.ToEntity() },
		MapUpdateDTO: func(req dto.UpdateCounterpartyRequest, old *counterparty.Counterparty) *counterparty.Counterparty {
			req.ApplyTo(old)
			return old
		},
		MapToDTO: func(row *counterparty.Counterparty) any { return dto.FromCounterparty(row) },
	})
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Use(func(c *gin.Context) {
		permissions := make(map[string]struct{})
		for _, permission := range appctx.GetUser(c.Request.Context()).Permissions {
			permissions[permission] = struct{}{}
		}
		c.Set("permissions_set", permissions)
		c.Next()
	})
	v1.RegisterCatalogRoutes(router.Group("/counterparties"), h, "counterparty")
	principal := appctx.UserContext{UserID: id.New().String(), Permissions: []string{"counterparty:read", "counterparty:create", "counterparty:update"}}
	_, err = tenant.Pool.Exec(context.Background(), `INSERT INTO auth_users(id,email,password_hash) VALUES($1,'presenter@test','fixture')`, principal.UserID)
	require.NoError(t, err)
	base := readaccess.WithService(tenant.Context(principal), reads)
	masked := security.WithFieldPolicies(base, map[string]*security.FieldPolicy{"counterparty:read": {AllowedFields: []string{"*", "-comment"}}})
	request := func(ctx context.Context, method, path, body, key string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set(middleware.HeaderIdempotencyKey, key)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	body := `{"code":"PRESENT","name":"Present","type":"customer","legalForm":"company","comment":"private-comment","attributes":{}}`
	first := request(masked, http.MethodPost, "/counterparties", body, "create-presenter")
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	require.NotContains(t, first.Body.String(), "private-comment")
	var created dto.CounterpartyResponse
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &created))
	require.NotEmpty(t, created.ID)
	var stored string
	require.NoError(t, tenant.Pool.QueryRow(context.Background(), `SELECT comment FROM cat_counterparties WHERE id=$1`, created.ID).Scan(&stored))
	require.Equal(t, "private-comment", stored, "presentation must not mutate the saved entity")
	var payload string
	require.NoError(t, tenant.Pool.QueryRow(context.Background(), `SELECT application_result::text FROM sys_idempotency WHERE idempotency_key='create-presenter'`).Scan(&payload))
	require.NotContains(t, payload, "private-comment", "only masked response is stored")
	for _, path := range []string{"/counterparties/" + created.ID, "/counterparties", "/counterparties/tree"} {
		response := request(masked, http.MethodGet, path, "", "")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.NotContains(t, response.Body.String(), "private-comment", path)
	}
	exported := request(masked, http.MethodPost, "/counterparties/export-list", `{"columns":[{"key":"name","label":"Name"},{"key":"comment","label":"Comment"}]}`, "")
	require.Equal(t, http.StatusOK, exported.Code, exported.Body.String())
	workbook, err := excelize.OpenReader(bytes.NewReader(exported.Body.Bytes()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, workbook.Close()) })
	for _, sheet := range workbook.GetSheetList() {
		rows, err := workbook.GetRows(sheet)
		require.NoError(t, err)
		require.NotContains(t, fmt.Sprint(rows), "private-comment")
	}
	update := fmt.Sprintf(`{"code":"PRESENT","name":"Present","type":"customer","legalForm":"company","comment":"changed-private","attributes":{},"version":%d}`, created.Version)
	updated := request(masked, http.MethodPut, "/counterparties/"+created.ID, update, "update-presenter")
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.NotContains(t, updated.Body.String(), "changed-private")
	moreRestricted := security.WithFieldPolicies(base, map[string]*security.FieldPolicy{"counterparty:read": {AllowedFields: []string{"*", "-comment", "-name"}}})
	replay := request(moreRestricted, http.MethodPost, "/counterparties", body, "create-presenter")
	require.Equal(t, http.StatusCreated, replay.Code, replay.Body.String())
	require.Equal(t, "true", replay.Header().Get(middleware.HeaderIdempotentReplayed))
	require.NotContains(t, replay.Body.String(), `"name":"Present"`)
	denied := security.WithPolicyRules(base, []security.PolicyRule{&security_profile.PolicyRule{ID: id.New(), EntityName: "counterparty", Actions: []string{"read"}, Enabled: true, Effect: "deny", Expression: "true"}})
	replay = request(denied, http.MethodPost, "/counterparties", body, "create-presenter")
	require.Equal(t, http.StatusForbidden, replay.Code, replay.Body.String())
	// Without a key, failure of read authorization must also roll back creation.
	rejected := request(denied, http.MethodPost, "/counterparties", strings.ReplaceAll(body, "PRESENT", "REJECTED"), "")
	require.Equal(t, http.StatusForbidden, rejected.Code, rejected.Body.String())
	var count int
	require.NoError(t, tenant.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM cat_counterparties`).Scan(&count))
	require.Equal(t, 1, count, "replay and failed projection cannot create extra rows")
	t.Run("existing cycle yields a controlled error", func(t *testing.T) {
		a, b := id.New(), id.New()
		_, err := tenant.Pool.Exec(context.Background(), `INSERT INTO cat_counterparties(id,code,name,is_folder) VALUES($1,'CYCLE-A','A',true),($2,'CYCLE-B','B',true)`, a, b)
		require.NoError(t, err)
		_, err = tenant.Pool.Exec(context.Background(), `UPDATE cat_counterparties SET parent_id=CASE WHEN id=$1 THEN $2 ELSE $1 END WHERE id IN ($1,$2)`, a, b)
		require.NoError(t, err)
		repo := catalog_repo.NewCounterpartyRepo()
		_, err = repo.GetPath(base, a)
		require.Error(t, err)
		require.Contains(t, err.Error(), "hierarchy contains a cycle")
		_, err = repo.GetTree(base, &a)
		require.Error(t, err)
		require.Contains(t, err.Error(), "hierarchy contains a cycle")
	})

}
