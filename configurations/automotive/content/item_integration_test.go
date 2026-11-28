//go:build integration

package content_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/configurations/automotive/content"
	"github.com/alex-bogatiuk/metapus/configurations/automotive/domain/catalogs/item_automotive"
	"github.com/alex-bogatiuk/metapus/configurations/trade/domain/catalogs/item"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/handlers"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/middleware"
	v1 "github.com/alex-bogatiuk/metapus/pkg/sdk/http/v1"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/metadata"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/numerator"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/readaccess"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/security"
)

// A tenant of the add-on holds the extension table of the base's items, and
// the items of the base's routes carry the add-on's fields: the normalized
// article is kept on every write and the list search finds a part by it,
// however the article is written.
func TestAnAutomotiveItemIsFoundByItsNormalizedArticle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := sdktest.NewTenant(t, content.Automotive())
	ctx := db.AdminContext()
	var table bool
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, content.ItemExtension.Table()).Scan(&table))
	require.True(t, table, "the add-on's migration creates the extension table")
	require.Equal(t, "cat_items__automotive", content.ItemExtension.Table())

	call := serveItems(t, db)
	status, filter := call("POST", "/catalog/items", `{"code":"IT-1","name":"Фильтр масляный","article":"w 712/75","manufacturer":"","unit":"шт","brand":" MANN-FILTER "}`)
	require.Equal(t, 201, status, filter)
	require.Equal(t, "W71275", filter["normalizedArticle"])
	require.Equal(t, "MANN-FILTER", filter["brand"], "the extension validates its fields")
	require.Equal(t, "w 712/75", filter["article"], "the base's article stays as written")
	status, pads := call("POST", "/catalog/items", `{"code":"IT-2","name":"Колодки","article":"0 986.494-524","unit":"комплект","manufacturer":"Bosch"}`)
	require.Equal(t, 201, status, pads)
	require.Equal(t, "0986494524", pads["normalizedArticle"])
	require.Equal(t, "Bosch", pads["manufacturer"], "the base's string manufacturer stays for any item")

	status, updated := call("PUT", "/catalog/items/"+pads["id"].(string), `{"code":"IT-2","name":"Колодки","article":"0 986 494 525","unit":"комплект","manufacturer":"Bosch","version":1}`)
	require.Equal(t, 200, status, updated)
	require.Equal(t, "0986494525", updated["normalizedArticle"], "the write hook keeps it on update")

	found := func(search string) []string {
		status, list := call("GET", "/catalog/items?limit=10&search="+url.QueryEscape(search), "")
		require.Equal(t, 200, status, list)
		var codes []string
		for _, row := range list["items"].([]any) {
			codes = append(codes, row.(map[string]any)["code"].(string))
		}
		return codes
	}
	require.Equal(t, []string{"IT-1"}, found("W712-75"))
	require.Equal(t, []string{"IT-1"}, found("w712/75"))
	require.Equal(t, []string{"IT-2"}, found("0986 494"), "a prefix of the article")
	require.Equal(t, []string{"IT-2"}, found("колодки"))
	require.Empty(t, found("0986494524"), "the old article is gone")

	status, list := call("GET", "/catalog/items?filter="+url.QueryEscape(`[{"field":"brand","operator":"eq","value":"MANN-FILTER"}]`), "")
	require.Equal(t, 200, status, list)
	require.Len(t, list["items"], 1)

	// The migration fills the rows of a base tenant's items with the same
	// normalization as the write hook.
	for _, article := range []string{"w 712/75", "0 986.494-524", "oc-90 ", "Ab\tc/1"} {
		var normalized string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT upper(regexp_replace($1, '[-/.[:space:]]', '', 'g'))`, article).Scan(&normalized))
		require.Equal(t, item_automotive.NormalizeArticle(article), normalized, article)
	}

	var indexed bool
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE tablename = 'cat_items__automotive' AND indexdef LIKE '%normalized_article text_pattern_ops%')`).Scan(&indexed))
	require.True(t, indexed, "the normalized article is indexed for the prefix search")
}

// serveItems mounts the items of the assembled application (platform, base
// and add-on) on a router that reads as an administrator of db.
func serveItems(t *testing.T, db *sdktest.Tenant) func(method, path, body string) (int, map[string]any) {
	t.Helper()
	reg := configuration.Compose(content.Automotive())
	metaReg := metadata.NewRegistry()
	def := metadata.Inspect(item.Item{}, item.Definition.Name, metadata.TypeCatalog)
	def.Key, def.TableName, def.ReadPermission = item.Definition.Key, item.Definition.Table, item.Definition.Permission()+":read"
	metaReg.Register(def)
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	t.Cleanup(engine.Stop)
	reads := postgres.NewAuthorizedReadService(metaReg, engine)
	ctx := readaccess.WithService(db.AdminContext(), reads)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(ctx)
		c.Set("permissions_set", map[string]struct{}{})
		c.Next()
	})
	router.Use(middleware.ErrorHandler())
	for _, catalog := range reg.Catalogs() {
		if catalog.EntityDefinition().Key == item.Definition.Key {
			v1.RegisterCatalogRoutes(router.Group("/catalog/"+catalog.RoutePrefix()),
				catalog.Build(v1.CatalogDeps{BaseHandler: handlers.NewBaseHandler(), Numerator: numerator.Noop()}), catalog.Permission())
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
