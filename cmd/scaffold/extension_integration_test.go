//go:build integration

package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/application/readaccess"
	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/numerator"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/counterparty"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/handlers"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/middleware"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/migration"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/testsupport"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
	"github.com/alex-bogatiuk/metapus/internal/platformcontent"
)

var extProbeDefinition = entity.Definition{Kind: entity.CatalogKind, Key: "ext_probe", Name: "ExtProbe", Route: "ext-probes", Table: "cat_ext_probes"}

// extProbe is a catalog of a base; extProbeShop is the add-on «shop»'s
// extension of it.
type extProbe struct {
	entity.Catalog
	Article string `db:"article" json:"article" meta:"label:Артикул"`
}

func (extProbe) EntityDefinition() entity.Definition { return extProbeDefinition }

type extProbeShop struct {
	Brand      string `db:"brand" json:"brand" meta:"label:Бренд"`
	Normalized string `db:"normalized" json:"normalized" meta:"label:Нормализованный артикул,readOnly:true"`
}

func normalizeProbe(article string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "", "/", "", ".", "").Replace(article))
}

// An add-on's extension of a declared catalog is served with the catalog:
// created, read, updated (an omitted field kept), listed and filtered by its
// fields, found by its normalized column, loaded from a file, written in the
// item's transaction (a failing extension rolls the item back) and under
// the catalog's field policies.
func TestCatalogExtensionIsServedWithTheCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := context.Background()
	scaffoldCatalog(t, tenant, "ext_probe")
	_, err := tenant.Pool.Exec(ctx, `ALTER TABLE cat_ext_probes ADD COLUMN article TEXT NOT NULL DEFAULT '';
		CREATE TABLE cat_ext_probes__shop (
			id UUID PRIMARY KEY REFERENCES cat_ext_probes(id) ON DELETE CASCADE,
			brand TEXT NOT NULL DEFAULT '' CHECK (brand <> 'BAD'),
			normalized TEXT NOT NULL DEFAULT '');
		CREATE INDEX ON cat_ext_probes__shop (normalized text_pattern_ops)`)
	require.NoError(t, err)

	extension := v1.Extend[extProbe, extProbeShop]("shop", "Магазин").
		OnWrite(func(_ context.Context, item *extProbe, ext *extProbeShop) error {
			if ext.Brand == "FAIL" {
				return apperror.NewValidation("бренд не принят").WithDetail("field", "brand")
			}
			ext.Normalized = normalizeProbe(item.Article)
			return nil
		}).
		Search("normalized", normalizeProbe)
	reg := v1.NewFactoryRegistry()
	reg.RegisterCatalog(v1.Catalog[extProbe](metadata.Presentation{Singular: "Деталь", Plural: "Детали"}))
	reg.RegisterExtension(extension)
	admin := appctx.UserContext{UserID: id.New().String(), IsAdmin: true, Roles: []string{"admin"}}
	call := serveExtended(t, tenant, reg, []declaredModel{{extProbe{}, extProbeDefinition}}, admin, nil)
	count := func(query string, args ...any) int {
		var n int
		require.NoError(t, tenant.Pool.QueryRow(ctx, query, args...).Scan(&n))
		return n
	}

	status, created := call("POST", "/catalog/ext-probes", `{"code":"P-1","name":"Фильтр масляный","article":"oc-90","brand":"MAHLE"}`)
	require.Equal(t, 201, status, created)
	require.Equal(t, "MAHLE", created["brand"])
	require.Equal(t, "OC90", created["normalized"], "the write hook computes the read-only field")
	probeID := created["id"].(string)
	var brand, normalized string
	require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT brand, normalized FROM cat_ext_probes__shop WHERE id=$1", probeID).Scan(&brand, &normalized))
	require.Equal(t, []string{"MAHLE", "OC90"}, []string{brand, normalized})

	status, _ = call("POST", "/catalog/ext-probes", `{"name":"Х","normalized":"X"}`)
	require.Equal(t, 400, status, "a read-only extension field is not writable")

	status, read := call("GET", "/catalog/ext-probes/"+probeID, "")
	require.Equal(t, 200, status, read)
	require.Equal(t, "MAHLE", read["brand"])

	status, updated := call("PUT", "/catalog/ext-probes/"+probeID, `{"code":"P-1","name":"Фильтр масляный","article":"OC 90/1","version":1}`)
	require.Equal(t, 200, status, updated)
	require.Equal(t, "MAHLE", updated["brand"], "an omitted extension field keeps its value")
	require.Equal(t, "OC901", updated["normalized"])
	require.Equal(t, float64(2), updated["version"])
	status, updated = call("PUT", "/catalog/ext-probes/"+probeID, `{"code":"P-1","name":"Фильтр масляный","article":"OC 90/1","brand":"KNECHT","version":2}`)
	require.Equal(t, 200, status, updated)
	require.Equal(t, "KNECHT", updated["brand"])
	require.Equal(t, float64(3), updated["version"], "an extension change is a change of the item")

	status, other := call("POST", "/catalog/ext-probes", `{"code":"P-2","name":"Колодки","article":"0 986/494","brand":"BOSCH"}`)
	require.Equal(t, 201, status, other)
	// An item stored before the add-on was installed has no extension row.
	legacyID := id.New().String()
	_, err = tenant.Pool.Exec(ctx, `INSERT INTO cat_ext_probes (id, code, name, article) VALUES ($1, 'P-0', 'Старая позиция', 'X-1')`, legacyID)
	require.NoError(t, err)
	status, legacy := call("GET", "/catalog/ext-probes/"+legacyID, "")
	require.Equal(t, 200, status, legacy)
	require.Equal(t, "", legacy["brand"], "it reads as the zero extension")

	names := func(query string) []string {
		status, list := call("GET", "/catalog/ext-probes?limit=10&orderBy=code&"+query, "")
		require.Equal(t, 200, status, list)
		var result []string
		for _, item := range list["items"].([]any) {
			row := item.(map[string]any)
			result = append(result, row["code"].(string)+":"+row["brand"].(string))
		}
		return result
	}
	filter := func(items string) string { return "filter=" + url.QueryEscape(items) }
	require.Equal(t, []string{"P-0:", "P-1:KNECHT", "P-2:BOSCH"}, names(""), "the list carries the extension")
	require.Equal(t, []string{"P-2:BOSCH"}, names(filter(`[{"field":"brand","operator":"eq","value":"BOSCH"}]`)))
	require.Equal(t, []string{"P-0:", "P-1:KNECHT"}, names(filter(`[{"field":"brand","operator":"neq","value":"BOSCH"}]`)), "an item without a row is not BOSCH")
	require.Equal(t, []string{"P-1:KNECHT"}, names(filter(`[{"field":"brand","operator":"contains","value":"kne"},{"field":"article","operator":"contains","value":"OC"}]`)))
	require.Equal(t, []string{"P-1:KNECHT"}, names("search="+url.QueryEscape("oc-90")), "the search finds the normalized article")
	require.Equal(t, []string{"P-2:BOSCH"}, names("search="+url.QueryEscape("0986.494")))
	require.Equal(t, []string{"P-2:BOSCH"}, names("search="+url.QueryEscape("колодки")), "and still the name")
	require.Empty(t, names("search="+url.QueryEscape("90/1X")))

	// A failing extension rolls the item back: its hook, its table's checks.
	items := func() int { return count("SELECT count(*) FROM cat_ext_probes") }
	before := items()
	status, _ = call("POST", "/catalog/ext-probes", `{"code":"P-9","name":"Отказ","brand":"FAIL"}`)
	require.Equal(t, 400, status)
	status, _ = call("POST", "/catalog/ext-probes", `{"code":"P-9","name":"Отказ","brand":"BAD"}`)
	require.Equal(t, 400, status, "the extension table's CHECK")
	require.Equal(t, before, items(), "the item was not stored without its extension")
	status, _ = call("PUT", "/catalog/ext-probes/"+probeID, `{"code":"P-1","name":"Переименован","article":"OC 90/1","brand":"BAD","version":3}`)
	require.Equal(t, 400, status)
	status, read = call("GET", "/catalog/ext-probes/"+probeID, "")
	require.Equal(t, 200, status, read)
	require.Equal(t, "Фильтр масляный", read["name"], "the item's own change rolled back with the extension's")
	require.Equal(t, float64(3), read["version"])

	// Loading from a file fills the extension by its label.
	file := func(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }
	body := func(v any) string {
		data, err := json.Marshal(v)
		require.NoError(t, err)
		return string(data)
	}
	content := file("Код;Наименование;Артикул;Бренд\nP-3;Фильтр воздушный;C 30 005;MANN\nP-4;Свеча;FR7DC+;BOSCH\n")
	status, preview := call("POST", "/catalog/ext-probes/import/preview", body(map[string]any{"fileName": "parts.csv", "content": content}))
	require.Equal(t, 200, status, preview)
	suggested := map[string]any{}
	for _, c := range preview["columns"].([]any) {
		column := c.(map[string]any)
		suggested[column["header"].(string)] = column["field"]
	}
	require.Equal(t, "brand", suggested["Бренд"], "the extension's field is offered by its label")
	mapping := map[string]string{"Код": "code", "Наименование": "name", "Артикул": "article", "Бренд": "brand"}
	status, loaded := call("POST", "/catalog/ext-probes/import", body(map[string]any{"fileName": "parts.csv", "content": content, "mapping": mapping}))
	require.Equal(t, 200, status, loaded)
	require.Equal(t, true, loaded["applied"], loaded)
	require.Equal(t, 1, count("SELECT count(*) FROM cat_ext_probes p JOIN cat_ext_probes__shop s USING (id) WHERE p.code='P-3' AND s.brand='MANN' AND s.normalized='C30005'"))
	renamed := file("Код;Наименование\nP-3;Фильтр воздушный (салон)\n")
	status, loaded = call("POST", "/catalog/ext-probes/import", body(map[string]any{"fileName": "parts.csv", "content": renamed,
		"mapping": map[string]string{"Код": "code", "Наименование": "name"}, "matchByCode": true}))
	require.Equal(t, 200, status, loaded)
	require.Equal(t, float64(1), loaded["updated"], loaded)
	require.Equal(t, 1, count("SELECT count(*) FROM cat_ext_probes__shop s JOIN cat_ext_probes p USING (id) WHERE p.code='P-3' AND s.brand='MANN'"), "a file without the column keeps the extension")
	failing := file("Код;Наименование;Бренд\nP-5;Ремень;GATES\nP-6;Ролик;BAD\n")
	status, loaded = call("POST", "/catalog/ext-probes/import", body(map[string]any{"fileName": "parts.csv", "content": failing,
		"mapping": map[string]string{"Код": "code", "Наименование": "name", "Бренд": "brand"}}))
	require.Equal(t, 200, status, loaded)
	require.Equal(t, false, loaded["applied"], loaded)
	require.Zero(t, count("SELECT count(*) FROM cat_ext_probes WHERE code IN ('P-5','P-6')"), "a row whose extension fails stores nothing")

	// The catalog's field policies cover the extension's fields.
	clerk := appctx.UserContext{UserID: id.New().String(), IsAdmin: true, Roles: []string{"admin"}}
	hidden := serveExtended(t, tenant, reg, []declaredModel{{extProbe{}, extProbeDefinition}}, clerk, map[string]*security.FieldPolicy{
		"ext_probe:read":  {AllowedFields: []string{"*", "-brand"}},
		"ext_probe:write": {AllowedFields: []string{"*", "-brand"}},
	})
	status, masked := hidden("GET", "/catalog/ext-probes/"+probeID, "")
	require.Equal(t, 200, status, masked)
	require.Equal(t, "", masked["brand"], "a hidden field is not read")
	require.Equal(t, "OC901", masked["normalized"])
	status, saved := hidden("PUT", "/catalog/ext-probes/"+probeID, `{"code":"P-1","name":"Фильтр масляный","article":"OC 90/1","brand":"","version":3}`)
	require.Equal(t, 200, status, saved)
	require.Equal(t, float64(4), saved["version"])
	require.Equal(t, 1, count("SELECT count(*) FROM cat_ext_probes__shop WHERE id=$1 AND brand='KNECHT'", probeID), "the masked form keeps the hidden value")
	status, _ = hidden("PUT", "/catalog/ext-probes/"+probeID, `{"code":"P-1","name":"Фильтр масляный","article":"OC 90/1","brand":"HACK","version":4}`)
	require.Equal(t, 403, status, "a hidden field is not written")
	status, _ = hidden("GET", "/catalog/ext-probes?"+filter(`[{"field":"brand","operator":"eq","value":"KNECHT"}]`), "")
	require.Equal(t, 403, status, "a hidden field is no filter oracle")

	status, _ = call("DELETE", "/catalog/ext-probes/"+other["id"].(string), "")
	require.Equal(t, 204, status)
	require.Zero(t, count("SELECT count(*) FROM cat_ext_probes__shop WHERE id=$1", other["id"]), "the extension row goes with the item")
}

// counterpartyShop is an add-on's extension of a platform catalog.
type counterpartyShop struct {
	Role string `db:"role" json:"role" meta:"label:Роль в автобизнесе"`
}

// The platform's own catalogs (counterparties, with their own service and
// repository) are extended the same way.
func TestPlatformCatalogExtensionIsServedWithTheCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := context.Background()
	_, err := tenant.Pool.Exec(ctx, `CREATE TABLE cat_counterparties__shop (
		id UUID PRIMARY KEY REFERENCES cat_counterparties(id) ON DELETE CASCADE,
		role TEXT NOT NULL DEFAULT '')`)
	require.NoError(t, err)
	reg := v1.NewFactoryRegistry()
	platformcontent.Register(reg, platformcontent.Integrations{})
	reg.RegisterExtension(v1.Extend[counterparty.Counterparty, counterpartyShop]("shop", "Магазин"))
	admin := appctx.UserContext{UserID: id.New().String(), IsAdmin: true, Roles: []string{"admin"}}
	call := serveExtended(t, tenant, reg, []declaredModel{{counterparty.Counterparty{}, counterparty.Definition}}, admin, nil)

	status, created := call("POST", "/catalog/counterparties", `{"code":"CP-1","name":"СТО «Поршень»","type":"customer","legalForm":"sole_trader","role":"partner_service"}`)
	require.Equal(t, 201, status, created)
	require.Equal(t, "partner_service", created["role"])
	status, read := call("GET", "/catalog/counterparties/"+created["id"].(string), "")
	require.Equal(t, 200, status, read)
	require.Equal(t, "partner_service", read["role"])
	status, list := call("GET", "/catalog/counterparties?filter="+url.QueryEscape(`[{"field":"role","operator":"eq","value":"partner_service"}]`), "")
	require.Equal(t, 200, status, list)
	require.Len(t, list["items"], 1)
}

// scaffoldCatalog creates the table of a catalog with the scaffold's migration.
func scaffoldCatalog(t *testing.T, tenant *testsupport.SecurityTenant, name string) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("pgx", tenant.Pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := t.TempDir()
	require.NoError(t, run([]string{"--name", name, "--type", "catalog"}, root, io.Discard))
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "extensions", name)), goose.WithTableName(migration.ExtensionsVersionTable), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
}

// serveExtended mounts the catalogs of models from reg on a router that reads
// as principal under policies, and returns a JSON caller.
func serveExtended(t *testing.T, tenant *testsupport.SecurityTenant, reg *v1.FactoryRegistry, models []declaredModel, principal appctx.UserContext, policies map[string]*security.FieldPolicy) func(method, path, body string) (int, map[string]any) {
	t.Helper()
	ctx := context.Background()
	metaReg := metadata.NewRegistry()
	served := map[string]bool{}
	for _, entry := range models {
		def := metadata.Inspect(entry.model, entry.def.Name, metadata.TypeCatalog)
		def.Key, def.TableName, def.ReadPermission = entry.def.Key, entry.def.Table, entry.def.Permission()+":read"
		metaReg.Register(def)
		served[entry.def.Key] = true
	}
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	t.Cleanup(engine.Stop)
	reads := postgres.NewAuthorizedReadService(metaReg, engine)
	_, err = tenant.Pool.Exec(ctx, `INSERT INTO auth_users(id,email,password_hash) VALUES($1,$2,'fixture')`, principal.UserID, principal.UserID+"@extended.test")
	require.NoError(t, err)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		requestCtx := readaccess.WithService(tenant.Context(principal), reads)
		c.Request = c.Request.WithContext(security.WithFieldPolicies(requestCtx, policies))
		c.Set("permissions_set", map[string]struct{}{})
		c.Next()
	})
	router.Use(middleware.ErrorHandler())
	base := handlers.NewBaseHandler()
	for _, catalog := range reg.Catalogs() {
		if served[catalog.EntityDefinition().Key] {
			v1.RegisterCatalogRoutes(router.Group("/catalog/"+catalog.RoutePrefix()), catalog.Build(v1.CatalogDeps{BaseHandler: base, Numerator: numerator.Noop()}), catalog.Permission())
		}
	}
	return func(method, path, body string) (int, map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		var out map[string]any
		if rec.Body.Len() > 0 {
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
		}
		return rec.Code, out
	}
}
