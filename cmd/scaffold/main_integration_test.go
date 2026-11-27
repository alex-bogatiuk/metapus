//go:build integration

package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/application/readaccess"
	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/numerator"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/auth"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
	_ "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/jsoncodec"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/handlers"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/middleware"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/document_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/migration"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/testsupport"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
	"github.com/gin-gonic/gin"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

type migrationDocument struct{ entity.Document }

func (*migrationDocument) GetDocumentType() string { return "ProbeDocument" }

func TestGeneratedMigrationsRoundTripAndDocumentHooksRollback(t *testing.T) {
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := context.Background()
	db, err := sql.Open("pgx", tenant.Pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := t.TempDir()
	for _, fixture := range []struct {
		name, kind string
		hierarchy  bool
	}{{"probe_catalog", "catalog", false}, {"probe_tree", "catalog", true}, {"probe_document", "document", false}} {
		args := []string{"--name", fixture.name, "--type", fixture.kind}
		if fixture.hierarchy {
			args = append(args, "--hierarchical")
		}
		require.NoError(t, run(args, root, io.Discard))
		provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "extensions", fixture.name)), goose.WithTableName(migration.ExtensionsVersionTable), goose.WithAllowOutofOrder(true))
		require.NoError(t, err)
		_, err = provider.Up(ctx)
		require.NoError(t, err)
		_, err = provider.ApplyVersion(ctx, provider.ListSources()[0].Version, false)
		require.NoError(t, err)
		_, err = provider.Up(ctx)
		require.NoError(t, err)
	}
	_, err = tenant.Pool.Exec(ctx, "CREATE TABLE probe_hook_effects (id UUID PRIMARY KEY)")
	require.NoError(t, err)
	repo := document_repo.NewBaseDocumentRepo("doc_probe_documents", postgres.ExtractDBColumns[migrationDocument](), func() *migrationDocument { return &migrationDocument{} })
	svc := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*migrationDocument]{Repo: repo, TxManager: tenant.Tx, Numerator: numerator.Noop(), EntityName: "probe_document"})
	svc.GetHooks().OnBeforeCreate(func(ctx context.Context, doc *migrationDocument) error {
		_, err := tenant.Tx.GetQuerier(ctx).Exec(ctx, "INSERT INTO probe_hook_effects(id) VALUES($1)", doc.ID)
		return err
	})
	failure := errors.New("mandatory effect failed")
	fail := true
	svc.GetHooks().On(domain.TransactionCreate, func(context.Context, *migrationDocument) error {
		if fail {
			return failure
		}
		return nil
	})
	callbacks := 0
	svc.GetHooks().OnAfterCreate(func(ctx context.Context, doc *migrationDocument) error {
		require.Nil(t, tenant.Tx.GetTx(ctx))
		var committed bool
		require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM doc_probe_documents WHERE id=$1)", doc.ID).Scan(&committed))
		require.True(t, committed)
		callbacks++
		return nil
	})
	mutationCtx := tenant.Context(appctx.UserContext{})
	doc := &migrationDocument{Document: entity.NewDocument()}
	doc.Number = "PROBE-ROLLBACK"
	// A command may catch the error and commit its own outcome. The failed
	// document attempt and Before hook must still roll back to a savepoint.
	require.NoError(t, tenant.Tx.RunInTransaction(mutationCtx, func(ctx context.Context) error {
		require.ErrorIs(t, svc.Create(ctx, doc), failure)
		return nil
	}))
	for _, table := range []string{"doc_probe_documents", "probe_hook_effects"} {
		var count int
		require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", doc.ID).Scan(&count))
		require.Zero(t, count)
	}
	require.Zero(t, callbacks)
	fail = false
	require.NoError(t, tenant.Tx.RunInTransaction(mutationCtx, func(ctx context.Context) error {
		require.NoError(t, svc.Create(ctx, doc))
		require.Zero(t, callbacks)
		return nil
	}))
	require.Equal(t, 1, callbacks)
}

// Models a scaffolded module grows into: the generated table plus business
// columns, registered with one declarative line each.
var declaredCatalogDefinition = entity.Definition{Kind: entity.CatalogKind, Key: "fleet_vehicle", Name: "FleetVehicle", Route: "fleet-vehicles", Table: "cat_fleet_vehicles", Code: entity.CodeRule{Prefix: "FV"}}

type declaredVehicle struct {
	entity.Catalog
	Plate    string `db:"plate" json:"plate" binding:"required"`
	Mileage  int    `db:"mileage" json:"mileage"`
	Verified bool   `db:"verified" json:"verified" meta:"readOnly:true"`
}

func (declaredVehicle) EntityDefinition() entity.Definition { return declaredCatalogDefinition }

var declaredDocumentDefinition = entity.Definition{Kind: entity.DocumentKind, Key: "fleet_waybill", Name: "FleetWaybill", Route: "fleet-waybill", Table: "doc_fleet_waybills", Number: entity.NumberRule{Prefix: "FW"}}

type declaredWaybill struct {
	entity.Document
	Distance int `db:"distance" json:"distance"`
}

func (declaredWaybill) EntityDefinition() entity.Definition { return declaredDocumentDefinition }
func (*declaredWaybill) GetDocumentType() string            { return declaredDocumentDefinition.Name }

func TestDeclaredEntitiesServeCRUDAndPostingOverTheirGeneratedTables(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := context.Background()
	db, err := sql.Open("pgx", tenant.Pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := t.TempDir()
	for _, fixture := range []struct{ name, kind string }{{"fleet_vehicle", "catalog"}, {"fleet_waybill", "document"}} {
		require.NoError(t, run([]string{"--name", fixture.name, "--type", fixture.kind}, root, io.Discard))
		provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "extensions", fixture.name)), goose.WithTableName(migration.ExtensionsVersionTable), goose.WithAllowOutofOrder(true))
		require.NoError(t, err)
		_, err = provider.Up(ctx)
		require.NoError(t, err)
	}
	_, err = tenant.Pool.Exec(ctx, `ALTER TABLE cat_fleet_vehicles ADD COLUMN plate TEXT NOT NULL DEFAULT '', ADD COLUMN mileage INT NOT NULL DEFAULT 0, ADD COLUMN verified BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE doc_fleet_waybills ADD COLUMN distance INT NOT NULL DEFAULT 0`)
	require.NoError(t, err)

	reg := v1.NewFactoryRegistry()
	reg.RegisterCatalog(v1.Catalog[declaredVehicle](metadata.Presentation{Singular: "ТС", Plural: "Транспорт"}))
	reg.RegisterDocument(v1.Document[declaredWaybill](metadata.Presentation{Singular: "Путевой лист", Plural: "Путевые листы"}))
	call := serveDeclared(t, tenant, reg, []declaredModel{{declaredVehicle{}, declaredCatalogDefinition}, {declaredWaybill{}, declaredDocumentDefinition}})

	status, created := call("POST", "/catalog/fleet-vehicles", `{"code":"V-1","name":"Газель","plate":"А001АА","mileage":12000}`)
	require.Equal(t, 201, status, created)
	require.Equal(t, "А001АА", created["plate"])
	require.Equal(t, false, created["verified"])
	vehicleID := created["id"].(string)

	status, _ = call("POST", "/catalog/fleet-vehicles", `{"name":"Без номера"}`)
	require.Equal(t, 400, status, "the model's binding:\"required\" applies to the request")
	status, _ = call("POST", "/catalog/fleet-vehicles", `{"name":"Х","plate":"Х","verified":true}`)
	require.Equal(t, 400, status, "a read-only field is not writable")

	status, read := call("GET", "/catalog/fleet-vehicles/"+vehicleID, "")
	require.Equal(t, 200, status, read)
	require.Equal(t, float64(12000), read["mileage"])
	status, updated := call("PUT", "/catalog/fleet-vehicles/"+vehicleID, `{"code":"V-1","name":"Газель","plate":"А001АА","mileage":15000,"version":1}`)
	require.Equal(t, 200, status, updated)
	require.Equal(t, float64(15000), updated["mileage"])
	require.Equal(t, float64(2), updated["version"])
	var mileage int
	require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT mileage FROM cat_fleet_vehicles WHERE id=$1", vehicleID).Scan(&mileage))
	require.Equal(t, 15000, mileage)
	status, list := call("GET", "/catalog/fleet-vehicles?limit=10", "")
	require.Equal(t, 200, status, list)
	require.Len(t, list["items"], 1)

	status, waybill := call("POST", "/document/fleet-waybill", `{"number":"W-1","date":"2026-11-16T10:00:00Z","distance":420}`)
	require.Equal(t, 201, status, waybill)
	require.Equal(t, float64(420), waybill["distance"])
	require.Equal(t, false, waybill["posted"])
	status, posted := call("POST", "/document/fleet-waybill/"+waybill["id"].(string)+"/post", "")
	require.Equal(t, 200, status, posted)
	require.Equal(t, true, posted["posted"])
}

type declaredModel struct {
	model any
	def   entity.Definition
}

// serveDeclared mounts the registry's declared catalogs and documents on a
// router that reads as an administrator of tenant, and returns a JSON caller.
func serveDeclared(t *testing.T, tenant *testsupport.SecurityTenant, reg *v1.FactoryRegistry, models []declaredModel) func(method, path, body string) (int, map[string]any) {
	t.Helper()
	return serveDeclaredAs(t, tenant, reg, models, appctx.UserContext{UserID: id.New().String(), IsAdmin: true, Roles: []string{"admin"}})
}

// serveDeclaredAs is serveDeclared for the user principal, who holds the
// permissions of principal.Permissions.
func serveDeclaredAs(t *testing.T, tenant *testsupport.SecurityTenant, reg *v1.FactoryRegistry, models []declaredModel, principal appctx.UserContext) func(method, path, body string) (int, map[string]any) {
	t.Helper()
	ctx := context.Background()
	metaReg := metadata.NewRegistry()
	for _, entry := range models {
		kind := metadata.TypeCatalog
		if entry.def.Kind == entity.DocumentKind {
			kind = metadata.TypeDocument
		}
		def := metadata.Inspect(entry.model, entry.def.Name, kind)
		def.Key, def.TableName, def.ReadPermission = entry.def.Key, entry.def.Table, entry.def.Permission()+":read"
		metaReg.Register(def)
	}
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	t.Cleanup(engine.Stop)
	reads := postgres.NewAuthorizedReadService(metaReg, engine)
	_, err = tenant.Pool.Exec(ctx, `INSERT INTO auth_users(id,email,password_hash) VALUES($1,$2,'fixture')`, principal.UserID, principal.UserID+"@declared.test")
	require.NoError(t, err)
	router := gin.New()
	granted := map[string]struct{}{}
	for _, permission := range principal.Permissions {
		granted[permission] = struct{}{}
	}
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(readaccess.WithService(tenant.Context(principal), reads))
		c.Set("permissions_set", granted)
		c.Next()
	})
	router.Use(middleware.ErrorHandler())
	base := handlers.NewBaseHandler()
	for _, catalog := range reg.Catalogs() {
		v1.RegisterCatalogRoutes(router.Group("/catalog/"+catalog.RoutePrefix()), catalog.Build(v1.CatalogDeps{BaseHandler: base, Numerator: numerator.Noop()}), catalog.Permission())
	}
	for _, document := range reg.Documents() {
		v1.RegisterDocumentRoutes(router.Group("/document/"+document.RoutePrefix()), document.Build(v1.DocumentDeps{BaseHandler: base, Numerator: numerator.Noop(), PostingEngine: posting.NewEngine(nil)}),
			document.Permission(), document.(v1.DocumentCapabilitiesProvider).DocumentCapabilities())
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

// A goods receipt with two table parts, scaffolded with --parts and grown
// with business columns: goods reference a product catalog, services do not.
// The goods part is served as "items" to keep the SQL key and the wire name
// apart.
var (
	productDefinition = entity.Definition{Kind: entity.CatalogKind, Key: "goods_product", Name: "GoodsProduct", Route: "goods-products", Table: "cat_goods_products"}
	receiptDefinition = entity.Definition{Kind: entity.DocumentKind, Key: "goods_receipt", Name: "GoodsReceipt", Route: "goods-receipt", Table: "doc_goods_receipts",
		Parts: []entity.Part{{Key: "goods", Field: "items"}, {Key: "services", Field: "services"}}}
)

type declaredProduct struct{ entity.Catalog }

func (declaredProduct) EntityDefinition() entity.Definition { return productDefinition }

type receiptItem struct {
	LineID    id.ID  `db:"line_id" json:"lineId"`
	LineNo    int    `db:"line_no" json:"lineNo" meta:"label:№"`
	ProductID id.ID  `db:"product_id" json:"productId" meta:"label:Товар,ref:goods_product" binding:"required"`
	Quantity  int    `db:"quantity" json:"quantity" meta:"label:Количество" binding:"min=1"`
	Batch     string `db:"batch" json:"batch" meta:"label:Партия,readOnly:true"`
}

type receiptService struct {
	LineID id.ID  `db:"line_id" json:"lineId"`
	LineNo int    `db:"line_no" json:"lineNo" meta:"label:№"`
	Title  string `db:"title" json:"title" meta:"label:Услуга" binding:"required"`
}

type declaredReceipt struct {
	entity.Document
	Warehouse string           `db:"warehouse" json:"warehouse"`
	Items     []receiptItem    `db:"-" json:"items" meta:"label:Товары"`
	Services  []receiptService `db:"-" json:"services" meta:"label:Услуги"`
}

func (declaredReceipt) EntityDefinition() entity.Definition { return receiptDefinition }
func (*declaredReceipt) GetDocumentType() string            { return receiptDefinition.Name }

// Validate is the model's business rule over its lines.
func (r *declaredReceipt) Validate(ctx context.Context) error {
	if len(r.Items) == 0 {
		return apperror.NewValidation("В поступлении нет товаров")
	}
	return r.Document.Validate(ctx)
}

func TestDeclaredDocumentStoresItsTablePartsWithTheHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := context.Background()
	db, err := sql.Open("pgx", tenant.Pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := t.TempDir()
	for _, args := range [][]string{{"--name", "goods_product"}, {"--name", "goods_receipt", "--type", "document", "--parts", "goods,services"}} {
		require.NoError(t, run(args, root, io.Discard))
		provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "extensions", args[1])), goose.WithTableName(migration.ExtensionsVersionTable), goose.WithAllowOutofOrder(true))
		require.NoError(t, err)
		_, err = provider.Up(ctx)
		require.NoError(t, err)
	}
	_, err = tenant.Pool.Exec(ctx, `ALTER TABLE doc_goods_receipts ADD COLUMN warehouse TEXT NOT NULL DEFAULT '';
		ALTER TABLE doc_goods_receipts__goods ADD COLUMN product_id UUID NOT NULL REFERENCES cat_goods_products(id), ADD COLUMN quantity INT NOT NULL, ADD COLUMN batch TEXT NOT NULL DEFAULT '';
		ALTER TABLE doc_goods_receipts__services ADD COLUMN title TEXT NOT NULL`)
	require.NoError(t, err)

	reg := v1.NewFactoryRegistry()
	reg.RegisterCatalog(v1.Catalog[declaredProduct](metadata.Presentation{Singular: "Товар", Plural: "Товары"}))
	reg.RegisterDocument(v1.Document[declaredReceipt](metadata.Presentation{Singular: "Поступление", Plural: "Поступления"}))
	call := serveDeclared(t, tenant, reg, []declaredModel{{declaredProduct{}, productDefinition}, {declaredReceipt{}, receiptDefinition}})

	product := func(name string) string {
		status, created := call("POST", "/catalog/goods-products", `{"code":"`+name+`","name":"`+name+`"}`)
		require.Equal(t, 201, status, created)
		return created["id"].(string)
	}
	nails, screws, bolts := product("Гвозди"), product("Шурупы"), product("Болты")
	lines := func(doc map[string]any, part string) []map[string]any {
		raw, _ := doc[part].([]any)
		out := make([]map[string]any, 0, len(raw))
		for _, line := range raw {
			out = append(out, line.(map[string]any))
		}
		return out
	}
	stored := func(table, docID string) (ids []string) {
		rows, err := tenant.Pool.Query(ctx, "SELECT line_id::text, line_no FROM "+table+" WHERE document_id=$1 ORDER BY line_no", docID)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var lineID string
			var lineNo int
			require.NoError(t, rows.Scan(&lineID, &lineNo))
			ids = append(ids, lineID)
			require.Equal(t, len(ids), lineNo, "stored lines are numbered by position")
		}
		require.NoError(t, rows.Err())
		return ids
	}

	status, body := call("POST", "/document/goods-receipt", `{"number":"GR-`+id.New().String()[:8]+`","date":"2026-11-17T09:00:00Z","warehouse":"Основной","services":[{"title":"Доставка"}]}`)
	require.Equal(t, 400, status, "the model's Validate sees the lines: a receipt without goods is refused")
	require.Contains(t, fmt.Sprint(body), "нет товаров")
	status, _ = call("POST", "/document/goods-receipt", `{"number":"GR-`+id.New().String()[:8]+`","date":"2026-11-17T09:00:00Z","items":[{"productId":"`+nails+`","quantity":0}]}`)
	require.Equal(t, 400, status, "line binding rules apply")
	status, body = call("POST", "/document/goods-receipt", `{"number":"GR-`+id.New().String()[:8]+`","date":"2026-11-17T09:00:00Z","items":[{"productId":"`+id.New().String()+`","quantity":1}]}`)
	require.Equal(t, 422, status, body)
	require.Contains(t, fmt.Sprint(body), "items.productId", "a dangling line reference names its line field")
	var orphans int
	require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT count(*) FROM doc_goods_receipts").Scan(&orphans))
	require.Zero(t, orphans, "a failed line write rolls the header back")

	status, receipt := call("POST", "/document/goods-receipt", `{"number":"GR-`+id.New().String()[:8]+`","date":"2026-11-17T09:00:00Z","warehouse":"Основной",
		"items":[{"productId":"`+nails+`","quantity":10},{"productId":"`+screws+`","quantity":5}],
		"services":[{"title":"Доставка"}]}`)
	require.Equal(t, 201, status, receipt)
	docID, number := receipt["id"].(string), receipt["number"].(string)
	status, body = call("POST", "/document/goods-receipt", `{"number":"`+number+`","date":"2026-11-17T09:00:00Z","warehouse":"Основной","items":[{"productId":"`+nails+`","quantity":1}]}`)
	require.Equal(t, 409, status, "a number another document holds is refused: %v", body)
	require.Contains(t, fmt.Sprint(body), "number")
	items := lines(receipt, "items")
	require.Len(t, items, 2)
	require.Equal(t, float64(1), items[0]["lineNo"])
	require.Equal(t, float64(2), items[1]["lineNo"])
	require.Equal(t, "Гвозди", items[0]["product"].(map[string]any)["name"], "line references resolve in the response")
	require.Equal(t, "Доставка", lines(receipt, "services")[0]["title"])
	goodsIDs := stored("doc_goods_receipts__goods", docID)
	require.Equal(t, []string{items[0]["lineId"].(string), items[1]["lineId"].(string)}, goodsIDs)
	_, err = tenant.Pool.Exec(ctx, "UPDATE doc_goods_receipts__goods SET batch='B-7' WHERE line_id=$1", goodsIDs[1])
	require.NoError(t, err)

	status, read := call("GET", "/document/goods-receipt/"+docID, "")
	require.Equal(t, 200, status, read)
	require.Equal(t, "B-7", lines(read, "items")[1]["batch"], "read-only line columns are served")
	require.Equal(t, "Шурупы", lines(read, "items")[1]["product"].(map[string]any)["name"])
	status, list := call("GET", "/document/goods-receipt?limit=10", "")
	require.Equal(t, 200, status, list)
	require.NotContains(t, list["items"].([]any)[0].(map[string]any), "items", "list rows carry no lines")

	// Keep the screws line, drop nails, add bolts; services are omitted and kept.
	status, updated := call("PUT", "/document/goods-receipt/"+docID, `{"number":"`+number+`","date":"2026-11-17T09:00:00Z","warehouse":"Основной","version":1,
		"items":[{"lineId":"`+goodsIDs[1]+`","productId":"`+screws+`","quantity":6},{"lineId":"`+goodsIDs[0]+`","productId":"`+bolts+`","quantity":3},{"productId":"`+bolts+`","quantity":1}]}`)
	require.Equal(t, 200, status, updated)
	items = lines(updated, "items")
	require.Len(t, items, 3)
	require.Equal(t, goodsIDs[1], items[0]["lineId"], "a line sent back with its lineId keeps it")
	require.Equal(t, goodsIDs[0], items[1]["lineId"])
	require.Equal(t, float64(1), items[0]["lineNo"], "lines are renumbered by position")
	require.Equal(t, float64(6), items[0]["quantity"])
	require.Equal(t, "B-7", items[0]["batch"], "a kept line keeps the read-only columns the server filled")
	require.Equal(t, "Болты", items[2]["product"].(map[string]any)["name"])
	newIDs := stored("doc_goods_receipts__goods", docID)
	require.Equal(t, []string{goodsIDs[1], goodsIDs[0], items[2]["lineId"].(string)}, newIDs)
	serviceIDs := stored("doc_goods_receipts__services", docID)
	require.Len(t, serviceIDs, 1, "an omitted part keeps its stored lines")

	filtered := func(productID string) int {
		query := url.QueryEscape(`[{"field":"items.product_id","operator":"eq","value":"` + productID + `"}]`)
		status, list := call("GET", "/document/goods-receipt?limit=10&filter="+query, "")
		require.Equal(t, 200, status, list)
		return len(list["items"].([]any))
	}
	require.Equal(t, 1, filtered(bolts), "a list filter reaches the lines")
	require.Zero(t, filtered(nails), "nails were dropped from the receipt")

	status, posted := call("POST", "/document/goods-receipt/"+docID+"/post", "")
	require.Equal(t, 200, status, posted)
	require.Equal(t, true, posted["posted"])
	postedIDs := stored("doc_goods_receipts__goods", docID)
	require.Equal(t, newIDs, postedIDs, "posting keeps the lines")

	status, cleared := call("PUT", "/document/goods-receipt/"+docID+"/repost", `{"number":"`+number+`","date":"2026-11-17T09:00:00Z","warehouse":"Основной","version":`+fmt.Sprint(posted["version"])+`,"services":[]}`)
	require.Equal(t, 200, status, cleared)
	require.Empty(t, lines(cleared, "services"))
	serviceIDs = stored("doc_goods_receipts__services", docID)
	require.Empty(t, serviceIDs, "an empty array clears the part")
	require.Len(t, lines(cleared, "items"), 3)
}

var calcInvoiceDefinition = entity.Definition{Kind: entity.DocumentKind, Key: "calc_invoice", Name: "CalcInvoice", Route: "calc-invoice", Table: "doc_calc_invoices",
	Parts: []entity.Part{{Key: "lines", Field: "lines"}}}

type calcInvoiceLine struct {
	LineID   id.ID            `db:"line_id" json:"lineId"`
	LineNo   int              `db:"line_no" json:"lineNo" meta:"label:№"`
	Quantity types.Quantity   `db:"quantity" json:"quantity" meta:"label:Количество"`
	Price    types.MinorUnits `db:"price" json:"price" meta:"label:Цена"`
	Amount   types.MinorUnits `db:"amount" json:"amount" meta:"label:Сумма,calc:quantity*price"`
}

type calcInvoice struct {
	entity.Document
	Rate  int64             `db:"rate" json:"rate" meta:"label:Ставка"`
	Base  types.MinorUnits  `db:"base" json:"base" meta:"label:База"`
	Fee   types.MinorUnits  `db:"fee" json:"fee" meta:"label:Комиссия,calc:base*rate/100"`
	Total types.MinorUnits  `db:"total" json:"total" meta:"label:Итого,calc:sum(lines.amount)"`
	Lines []calcInvoiceLine `db:"-" json:"lines" meta:"label:Строки"`
}

func (calcInvoice) EntityDefinition() entity.Definition { return calcInvoiceDefinition }
func (*calcInvoice) GetDocumentType() string            { return calcInvoiceDefinition.Name }

// A declared document's calculated fields are the server's: requests may not
// set them, and every write stores them computed from the stored inputs.
func TestDeclaredDocumentStoresItsCalculatedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := context.Background()
	db, err := sql.Open("pgx", tenant.Pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := t.TempDir()
	require.NoError(t, run([]string{"--name", "calc_invoice", "--type", "document", "--parts", "lines"}, root, io.Discard))
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "extensions", "calc_invoice")), goose.WithTableName(migration.ExtensionsVersionTable), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	_, err = tenant.Pool.Exec(ctx, `ALTER TABLE doc_calc_invoices ADD COLUMN rate BIGINT NOT NULL DEFAULT 0, ADD COLUMN base BIGINT NOT NULL DEFAULT 0, ADD COLUMN fee BIGINT NOT NULL DEFAULT 0, ADD COLUMN total BIGINT NOT NULL DEFAULT 0;
		ALTER TABLE doc_calc_invoices__lines ADD COLUMN quantity BIGINT NOT NULL, ADD COLUMN price BIGINT NOT NULL, ADD COLUMN amount BIGINT NOT NULL`)
	require.NoError(t, err)

	meta := metadata.Inspect(calcInvoice{}, calcInvoiceDefinition.Name, metadata.TypeDocument)
	var amount metadata.FieldDef
	for _, column := range meta.TableParts[0].Columns {
		if column.Name == "amount" {
			amount = column
		}
	}
	require.Equal(t, "quantity*price", amount.Calc, "metadata carries the formula for forms")
	require.True(t, amount.ReadOnly)

	reg := v1.NewFactoryRegistry()
	reg.RegisterDocument(v1.Document[calcInvoice](metadata.Presentation{Singular: "Счёт", Plural: "Счета"}))
	call := serveDeclared(t, tenant, reg, []declaredModel{{calcInvoice{}, calcInvoiceDefinition}})

	number := "CI-" + id.New().String()[:8]
	status, body := call("POST", "/document/calc-invoice", `{"number":"`+number+`","date":"2026-11-17T09:00:00Z","rate":3,"base":"10001",
		"lines":[{"quantity":1.5,"price":"1999"},{"quantity":"2","price":"500"}]}`)
	require.Equal(t, 201, status, body)
	require.Equal(t, "300", fmt.Sprint(body["fee"]), "10001 × 3 / 100 = 300.03 → 300; money is a string of minor units")
	lines := body["lines"].([]any)
	require.Equal(t, "2999", fmt.Sprint(lines[0].(map[string]any)["amount"]), "1.5 × 1999 = 2998.5 → 2999, half away from zero")
	require.Equal(t, "1000", fmt.Sprint(lines[1].(map[string]any)["amount"]))
	require.Equal(t, "3999", fmt.Sprint(body["total"]), "the document's total is the sum of its lines' calculated amounts")
	docID := body["id"].(string)
	storedAmounts := func() (out []int64) {
		rows, err := tenant.Pool.Query(ctx, "SELECT amount FROM doc_calc_invoices__lines WHERE document_id=$1 ORDER BY line_no", docID)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var value int64
			require.NoError(t, rows.Scan(&value))
			out = append(out, value)
		}
		require.NoError(t, rows.Err())
		return out
	}
	require.Equal(t, []int64{2999, 1000}, storedAmounts())
	var fee int64
	require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT fee FROM doc_calc_invoices WHERE id=$1", docID).Scan(&fee))
	require.EqualValues(t, 300, fee)

	status, body = call("POST", "/document/calc-invoice", `{"number":"CI-`+id.New().String()[:8]+`","date":"2026-11-17T09:00:00Z",
		"lines":[{"quantity":1,"price":"100","amount":"1"}]}`)
	require.Equal(t, 400, status, "a request may not set a calculated field: %v", body)

	// A kept line's inputs change: its amount follows on the server.
	firstLine := lines[0].(map[string]any)["lineId"].(string)
	status, body = call("PUT", "/document/calc-invoice/"+docID, `{"number":"`+number+`","date":"2026-11-17T09:00:00Z","rate":3,"base":"10001","version":1,
		"lines":[{"lineId":"`+firstLine+`","quantity":3,"price":"1999"}]}`)
	require.Equal(t, 200, status, body)
	require.Equal(t, []int64{5997}, storedAmounts())
	var total int64
	require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT total FROM doc_calc_invoices WHERE id=$1", docID).Scan(&total))
	require.EqualValues(t, 5997, total, "the stored total follows the lines")
}

var returnDefinition = entity.Definition{Kind: entity.DocumentKind, Key: "goods_return", Name: "GoodsReturn", Route: "goods-return", Table: "doc_goods_returns",
	Parts: []entity.Part{{Key: "goods", Field: "items"}}}

type returnItem struct {
	LineID    id.ID `db:"line_id" json:"lineId"`
	LineNo    int   `db:"line_no" json:"lineNo" meta:"label:№"`
	ProductID id.ID `db:"product_id" json:"productId" meta:"label:Товар,ref:goods_product" binding:"required"`
	Quantity  int   `db:"quantity" json:"quantity" meta:"label:Количество" binding:"min=1"`
}

type declaredReturn struct {
	entity.Document
	Warehouse string       `db:"warehouse" json:"warehouse"`
	Items     []returnItem `db:"-" json:"items" meta:"label:Товары"`
}

func (declaredReturn) EntityDefinition() entity.Definition { return returnDefinition }
func (*declaredReturn) GetDocumentType() string            { return returnDefinition.Name }

// A basis rule serves a draft of one declared document filled from another:
// the source read as its GET reads it, the draft's basis set, nothing stored.
// The form saves the draft with its basis.
func TestDeclaredBasisFillsADraftFromAnotherDocument(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := context.Background()
	db, err := sql.Open("pgx", tenant.Pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := t.TempDir()
	for _, args := range [][]string{{"--name", "goods_product"}, {"--name", "goods_receipt", "--type", "document", "--parts", "goods,services"}, {"--name", "goods_return", "--type", "document", "--parts", "goods"}} {
		require.NoError(t, run(args, root, io.Discard))
		provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "extensions", args[1])), goose.WithTableName(migration.ExtensionsVersionTable), goose.WithAllowOutofOrder(true))
		require.NoError(t, err)
		_, err = provider.Up(ctx)
		require.NoError(t, err)
	}
	_, err = tenant.Pool.Exec(ctx, `ALTER TABLE doc_goods_receipts ADD COLUMN warehouse TEXT NOT NULL DEFAULT '';
		ALTER TABLE doc_goods_receipts__goods ADD COLUMN product_id UUID NOT NULL REFERENCES cat_goods_products(id), ADD COLUMN quantity INT NOT NULL, ADD COLUMN batch TEXT NOT NULL DEFAULT '';
		ALTER TABLE doc_goods_receipts__services ADD COLUMN title TEXT NOT NULL;
		ALTER TABLE doc_goods_returns ADD COLUMN warehouse TEXT NOT NULL DEFAULT '';
		ALTER TABLE doc_goods_returns__goods ADD COLUMN product_id UUID NOT NULL REFERENCES cat_goods_products(id), ADD COLUMN quantity INT NOT NULL`)
	require.NoError(t, err)

	reg := v1.NewFactoryRegistry()
	reg.RegisterCatalog(v1.Catalog[declaredProduct](metadata.Presentation{Singular: "Товар", Plural: "Товары"}))
	reg.RegisterDocument(v1.Document[declaredReceipt](metadata.Presentation{Singular: "Поступление", Plural: "Поступления"}))
	reg.RegisterDocument(v1.Document[declaredReturn](metadata.Presentation{Singular: "Возврат", Plural: "Возвраты"}))
	reg.RegisterBasis(v1.Basis("Возврат поставщику", func(_ context.Context, receipt *declaredReceipt, ret *declaredReturn) error {
		ret.Warehouse = receipt.Warehouse
		for _, item := range receipt.Items {
			ret.Items = append(ret.Items, returnItem{LineID: id.New(), LineNo: len(ret.Items) + 1, ProductID: item.ProductID, Quantity: item.Quantity})
		}
		return nil
	}))
	require.Panics(t, func() {
		reg.RegisterBasis(v1.Basis("Ещё раз", func(context.Context, *declaredReceipt, *declaredReturn) error { return nil }))
	})
	call := serveDeclared(t, tenant, reg, []declaredModel{{declaredProduct{}, productDefinition}, {declaredReceipt{}, receiptDefinition}, {declaredReturn{}, returnDefinition}})

	status, product := call("POST", "/catalog/goods-products", `{"code":"N","name":"Гвозди"}`)
	require.Equal(t, 201, status, product)
	status, receipt := call("POST", "/document/goods-receipt", `{"number":"GR-`+id.New().String()[:8]+`","date":"2026-11-17T09:00:00Z","warehouse":"Основной",
		"items":[{"productId":"`+product["id"].(string)+`","quantity":10}]}`)
	require.Equal(t, 201, status, receipt)
	receiptID := receipt["id"].(string)

	status, draft := call("GET", "/document/goods-return/based-on/goods_receipt/"+receiptID, "")
	require.Equal(t, 200, status, draft)
	require.Equal(t, "GoodsReceipt", draft["basisType"])
	require.Equal(t, receiptID, draft["basisId"])
	require.Equal(t, "Основной", draft["warehouse"])
	require.Equal(t, "", draft["number"], "the draft is numbered when saved")
	items := draft["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, "Гвозди", items[0].(map[string]any)["product"].(map[string]any)["name"], "the draft's references resolve")
	var returns int
	require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT count(*) FROM doc_goods_returns").Scan(&returns))
	require.Zero(t, returns, "a draft is not stored")

	status, _ = call("GET", "/document/goods-return/based-on/goods_receipt/"+id.New().String(), "")
	require.Equal(t, 404, status, "a missing source is not found")
	status, _ = call("GET", "/document/goods-return/based-on/goods_receipt/not-an-id", "")
	require.Equal(t, 400, status)

	// The form saves the draft as it came, basis included.
	body, err := json.Marshal(map[string]any{
		"number": "RT-" + id.New().String()[:8], "date": "2026-11-18T09:00:00Z", "warehouse": draft["warehouse"], "basisType": draft["basisType"], "basisId": draft["basisId"],
		"items": []any{map[string]any{"productId": product["id"], "quantity": 10}},
	})
	require.NoError(t, err)
	status, saved := call("POST", "/document/goods-return", string(body))
	require.Equal(t, 201, status, saved)
	var basisType, basisID string
	require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT basis_type, basis_id::text FROM doc_goods_returns WHERE id=$1", saved["id"]).Scan(&basisType, &basisID))
	require.Equal(t, "GoodsReceipt", basisType)
	require.Equal(t, receiptID, basisID)
}

// A service ticket with a status model: new, in work, closed. Reopening a
// closed ticket takes its own permission.
type ticketStatus string

var ticketDefinition = entity.Definition{Kind: entity.DocumentKind, Key: "service_ticket", Name: "ServiceTicket", Route: "service-ticket", Table: "doc_service_tickets",
	Statuses: entity.StatusModel{
		Column: "status", Initial: "new",
		Values: []entity.StatusValue{{Value: "new", Label: "Новая"}, {Value: "work", Label: "В работе"}, {Value: "closed", Label: "Закрыта"}},
		Transitions: []entity.StatusTransition{
			{From: []string{"new"}, To: "work"},
			{From: []string{"new", "work"}, To: "closed"},
			{From: []string{"closed"}, To: "work", Permission: "reopen", Label: "Возобновление заявки"},
		},
	}}

type serviceTicket struct {
	entity.Document
	Status ticketStatus `db:"status" json:"status" meta:"label:Статус"`
}

func (serviceTicket) EntityDefinition() entity.Definition { return ticketDefinition }
func (*serviceTicket) GetDocumentType() string            { return ticketDefinition.Name }

// The status model of a declared document holds on every write: a new
// document starts in the initial status, a change no transition allows is
// refused, a transition with a permission is closed to users without it,
// and each change is in the document's status history.
func TestDeclaredDocumentFollowsItsStatusModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := context.Background()
	db, err := sql.Open("pgx", tenant.Pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := t.TempDir()
	require.NoError(t, run([]string{"--name", "service_ticket", "--type", "document"}, root, io.Discard))
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "extensions", "service_ticket")), goose.WithTableName(migration.ExtensionsVersionTable), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	_, err = tenant.Pool.Exec(ctx, `ALTER TABLE doc_service_tickets ADD COLUMN status TEXT NOT NULL`)
	require.NoError(t, err)

	reg := v1.NewFactoryRegistry()
	reg.RegisterDocument(v1.Document[serviceTicket](metadata.Presentation{Singular: "Заявка", Plural: "Заявки"}))
	models := []declaredModel{{serviceTicket{}, ticketDefinition}}
	admin := serveDeclared(t, tenant, reg, models)
	clerk := serveDeclaredAs(t, tenant, reg, models, appctx.UserContext{UserID: id.New().String(),
		Permissions: []string{"document:service_ticket:read", "document:service_ticket:create", "document:service_ticket:update"}})

	permissions := map[string]string{}
	for _, p := range reg.Permissions() {
		permissions[p.Code] = p.Name
	}
	require.Equal(t, "Заявки: возобновление заявки", permissions["document:service_ticket:reopen"])

	number := func() string { return "T-" + id.New().String()[24:] }
	status, ticket := clerk("POST", "/document/service-ticket", `{"number":"`+number()+`","date":"2026-11-20T09:00:00Z"}`)
	require.Equal(t, 201, status, ticket)
	require.Equal(t, "new", ticket["status"], "a document written without a status starts in the initial one")
	ticketID := ticket["id"].(string)
	change := func(call func(string, string, string) (int, map[string]any), to string, version float64) (int, map[string]any) {
		return call("PUT", "/document/service-ticket/"+ticketID, fmt.Sprintf(`{"number":%q,"date":"2026-11-20T09:00:00Z","status":%q,"version":%v}`, ticket["number"], to, version))
	}
	refused := func(code int, body map[string]any, want string) {
		t.Helper()
		require.Equal(t, want, body["code"], body)
		require.NotEqual(t, 200, code)
	}

	status, body := change(clerk, "work", 1)
	require.Equal(t, 200, status, body)
	code, body := change(clerk, "new", 2)
	refused(code, body, domain.CodeStatusTransitionNotAllowed)
	require.Contains(t, body["message"], "из статуса «В работе» в статус «Новая»")
	code, body = change(clerk, "lost", 2)
	refused(code, body, domain.CodeStatusUnknown)
	status, body = change(clerk, "closed", 2)
	require.Equal(t, 200, status, body)
	code, body = change(clerk, "work", 3)
	require.Equal(t, 403, code, body)
	require.Contains(t, body["message"], "«В работе»")
	status, body = change(admin, "work", 3)
	require.Equal(t, 200, status, "an administrator reopens the ticket: %v", body)

	var stored string
	require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT status FROM doc_service_tickets WHERE id=$1", ticketID).Scan(&stored))
	require.Equal(t, "work", stored)

	status, history := clerk("GET", "/document/service-ticket/"+ticketID+"/status-history", "")
	require.Equal(t, 200, status, history)
	items := history["items"].([]any)
	require.Len(t, items, 4, "refused changes leave no rows")
	var trail []string
	for _, item := range items {
		row := item.(map[string]any)
		trail = append(trail, fmt.Sprintf("%v→%v", row["previousStatusLabel"], row["statusLabel"]))
		require.NotEmpty(t, row["changedByName"], row)
	}
	require.Equal(t, []string{"<nil>→Новая", "Новая→В работе", "В работе→Закрыта", "Закрыта→В работе"}, trail)

	status, direct := clerk("POST", "/document/service-ticket", `{"number":"`+number()+`","date":"2026-11-20T09:00:00Z","status":"closed"}`)
	require.Equal(t, 201, status, "a new document may start where a transition from the initial status leads: %v", direct)
	status, history = clerk("GET", "/document/service-ticket/"+direct["id"].(string)+"/status-history", "")
	require.Equal(t, 200, status, history)
	require.Len(t, history["items"], 1)

	status, _ = clerk("GET", "/document/service-ticket/"+id.New().String()+"/status-history", "")
	require.Equal(t, 404, status, "the history of a missing document is not found")
}

// The period lock refuses every change of a document dated in the closed
// period, through the one repository every document write takes: creating
// it, changing it, moving it into the period, posting, unposting and
// marking it. Holders of the exception and administrators work there.
func TestPeriodLockRefusesChangesInTheClosedPeriod(t *testing.T) {
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := context.Background()
	db, err := sql.Open("pgx", tenant.Pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := t.TempDir()
	require.NoError(t, run([]string{"--name", "probe_document", "--type", "document"}, root, io.Discard))
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "extensions", "probe_document")), goose.WithTableName(migration.ExtensionsVersionTable), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	_, err = tenant.Pool.Exec(ctx, `UPDATE sys_settings SET general = '{"timezone": "Europe/Moscow"}';
		INSERT INTO sys_period_locks (locked_until) VALUES ('2026-10-31')`)
	require.NoError(t, err)

	repo := document_repo.NewBaseDocumentRepo("doc_probe_documents", postgres.ExtractDBColumns[migrationDocument](), func() *migrationDocument { return &migrationDocument{} })
	svc := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*migrationDocument]{
		Repo: repo, TxManager: tenant.Tx, Numerator: numerator.Noop(), EntityName: "probe_document", PostingEngine: posting.NewEngine(nil),
	})
	clerk := tenant.Context(appctx.UserContext{UserID: id.New().String()})
	accountant := tenant.Context(appctx.UserContext{UserID: id.New().String(), Permissions: []string{auth.PermissionClosedPeriodEdit}})
	document := func(date time.Time) *migrationDocument {
		doc := &migrationDocument{Document: entity.NewDocument()}
		doc.Number, doc.Date = "LOCK-"+doc.ID.String()[24:], date
		return doc
	}
	closed := func(err error) {
		t.Helper()
		appErr, ok := apperror.AsAppError(err)
		require.True(t, ok, "%v", err)
		require.Equal(t, apperror.CodePeriodClosed, appErr.Code, appErr.Message)
	}
	october := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

	closed(svc.Create(clerk, document(october)))
	// 21:30 UTC on 31 October is 1 November in Moscow: open.
	require.NoError(t, svc.Create(clerk, document(time.Date(2026, 10, 31, 21, 30, 0, 0, time.UTC))))

	// The accountant enters an October document; the clerk can do nothing
	// with it.
	old := document(october)
	require.NoError(t, svc.Create(accountant, old))
	stored, err := svc.GetByID(clerk, old.ID)
	require.NoError(t, err)
	stored.Description = "исправление"
	closed(svc.Update(clerk, stored))
	closed(svc.Post(clerk, old.ID))
	closed(svc.SetDeletionMark(clerk, old.ID, true))
	require.NoError(t, svc.Post(accountant, old.ID))
	closed(svc.Unpost(clerk, old.ID))
	require.NoError(t, svc.Unpost(accountant, old.ID))

	// A November document cannot be moved into October.
	november := document(time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC))
	require.NoError(t, svc.Create(clerk, november))
	moved, err := svc.GetByID(clerk, november.ID)
	require.NoError(t, err)
	moved.Date = october
	closed(svc.Update(clerk, moved))

	admin := tenant.Context(appctx.UserContext{UserID: id.New().String(), IsAdmin: true})
	require.NoError(t, svc.SetDeletionMark(admin, old.ID, true))

	// The settings section moves and clears the date.
	settingsRepo := postgres.NewSettingsRepo()
	current, err := settingsRepo.Get(admin)
	require.NoError(t, err)
	require.Equal(t, "2026-10-31", current.PeriodLock.Date)
	updated, err := settingsRepo.UpdateSection(admin, "periodLock", json.RawMessage(`{"date":"2026-09-30"}`), current.Version)
	require.NoError(t, err)
	require.Equal(t, "2026-09-30", updated.PeriodLock.Date)
	require.Equal(t, current.Version+1, updated.Version)
	require.NoError(t, svc.Create(clerk, document(october)), "October is open once the date moves back")
	_, err = settingsRepo.UpdateSection(admin, "periodLock", json.RawMessage(`{"date":"30.09.2026"}`), updated.Version)
	require.Error(t, err)
	_, err = settingsRepo.UpdateSection(admin, "periodLock", json.RawMessage(`{"date":""}`), current.Version)
	require.Error(t, err, "a stale version conflicts")
	cleared, err := settingsRepo.UpdateSection(admin, "periodLock", json.RawMessage(`{"date":""}`), updated.Version)
	require.NoError(t, err)
	require.Empty(t, cleared.PeriodLock.Date)
	require.NoError(t, svc.Create(clerk, document(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))))
}

var (
	importUnitDefinition = entity.Definition{Kind: entity.CatalogKind, Key: "import_unit", Name: "ImportUnit", Route: "import-units", Table: "cat_import_units"}
	importItemDefinition = entity.Definition{Kind: entity.CatalogKind, Key: "import_item", Name: "ImportItem", Route: "import-items", Table: "cat_import_items",
		Hierarchy: entity.CatalogMeta{Hierarchical: true}}
)

type importUnit struct{ entity.Catalog }

func (importUnit) EntityDefinition() entity.Definition { return importUnitDefinition }

type importItem struct {
	entity.Catalog
	Article string `db:"article" json:"article" meta:"label:Артикул"`
	UnitID  id.ID  `db:"unit_id" json:"unitId" meta:"label:Единица,ref:import_unit" binding:"required"`
	Weight  int    `db:"weight" json:"weight" meta:"label:Вес"`
}

func (importItem) EntityDefinition() entity.Definition { return importItemDefinition }

// A declared catalog loads items from a file: the preview suggests the
// fields of the columns, a dry run checks every row and stores nothing, a
// file with bad rows stores nothing and names them, and loading again with
// matchByCode updates the items it finds by code.
func TestDeclaredCatalogLoadsItemsFromAFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := context.Background()
	db, err := sql.Open("pgx", tenant.Pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := t.TempDir()
	for _, args := range [][]string{{"--name", "import_unit"}, {"--name", "import_item", "--hierarchical"}} {
		require.NoError(t, run(args, root, io.Discard))
		provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "extensions", args[1])), goose.WithTableName(migration.ExtensionsVersionTable), goose.WithAllowOutofOrder(true))
		require.NoError(t, err)
		_, err = provider.Up(ctx)
		require.NoError(t, err)
	}
	_, err = tenant.Pool.Exec(ctx, `ALTER TABLE cat_import_items ADD COLUMN article TEXT NOT NULL DEFAULT '',
		ADD COLUMN unit_id UUID REFERENCES cat_import_units(id), ADD COLUMN weight INT NOT NULL DEFAULT 0`)
	require.NoError(t, err)

	reg := v1.NewFactoryRegistry()
	reg.RegisterCatalog(v1.Catalog[importUnit](metadata.Presentation{Singular: "Единица", Plural: "Единицы"}))
	reg.RegisterCatalog(v1.Catalog[importItem](metadata.Presentation{Singular: "Номенклатура", Plural: "Номенклатура"}))
	call := serveDeclared(t, tenant, reg, []declaredModel{{importUnit{}, importUnitDefinition}, {importItem{}, importItemDefinition}})
	status, unit := call("POST", "/catalog/import-units", `{"code":"PCS","name":"шт"}`)
	require.Equal(t, 201, status, unit)
	status, folder := call("POST", "/catalog/import-items", `{"code":"G-1","name":"Масла","isFolder":true,"unitId":"`+unit["id"].(string)+`"}`)
	require.Equal(t, 201, status, folder)

	file := func(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }
	body := func(v any) string {
		data, err := json.Marshal(v)
		require.NoError(t, err)
		return string(data)
	}
	good := file("Код;Наименование;Артикул;Ед.;Вес;Группа\nIT-1;Масло 5W-30;152648;шт;5;Масла\nIT-2;Колодки;0986;PCS;2;\n")

	status, preview := call("POST", "/catalog/import-items/import/preview", body(map[string]any{"fileName": "items.csv", "content": good}))
	require.Equal(t, 200, status, preview)
	require.Equal(t, float64(2), preview["total"])
	suggested := map[string]any{}
	for _, c := range preview["columns"].([]any) {
		column := c.(map[string]any)
		suggested[column["header"].(string)] = column["field"]
	}
	require.Equal(t, map[string]any{"Код": "code", "Наименование": "name", "Артикул": "article", "Ед.": "", "Вес": "weight", "Группа": "parentId"}, suggested)

	mapping := map[string]string{"Код": "code", "Наименование": "name", "Артикул": "article", "Ед.": "unitId", "Вес": "weight", "Группа": "parentId"}
	items := func() int {
		var n int
		require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT count(*) FROM cat_import_items WHERE NOT is_folder").Scan(&n))
		return n
	}
	status, checked := call("POST", "/catalog/import-items/import", body(map[string]any{"fileName": "items.csv", "content": good, "mapping": mapping, "dryRun": true}))
	require.Equal(t, 200, status, checked)
	require.Equal(t, float64(2), checked["created"])
	require.Equal(t, false, checked["applied"])
	require.Zero(t, items(), "a dry run stores nothing")

	bad := file("Код;Наименование;Артикул;Ед.;Вес;Группа\nIT-1;Масло;1;кг;5;\nIT-2;Колодки;2;шт;много;\nIT-3;;3;шт;1;\nIT-4;Фильтр;4;шт;1;Нет такой\n")
	status, refused := call("POST", "/catalog/import-items/import", body(map[string]any{"fileName": "items.csv", "content": bad, "mapping": mapping}))
	require.Equal(t, 200, status, refused)
	require.Equal(t, false, refused["applied"])
	var problems []string
	for _, e := range refused["errors"].([]any) {
		row := e.(map[string]any)
		problems = append(problems, fmt.Sprintf("%v %v: %v", row["line"], row["column"], row["message"]))
	}
	require.Equal(t, []string{
		"2 Ед.: не найдено «кг»",
		"3 Вес: не целое число: «много»",
		"4 Наименование: обязательное поле не заполнено",
		"5 Группа: нет группы «Нет такой»",
	}, problems)
	require.Zero(t, items(), "a file with errors stores nothing")

	status, loaded := call("POST", "/catalog/import-items/import", body(map[string]any{"fileName": "items.csv", "content": good, "mapping": mapping}))
	require.Equal(t, 200, status, loaded)
	require.Equal(t, true, loaded["applied"])
	require.Equal(t, 2, items())
	var parent, unitID, article string
	var weight int
	require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT parent_id::text, unit_id::text, article, weight FROM cat_import_items WHERE code='IT-1'").Scan(&parent, &unitID, &article, &weight))
	require.Equal(t, folder["id"], parent)
	require.Equal(t, unit["id"], unitID)
	require.Equal(t, "152648", article)
	require.Equal(t, 5, weight)

	// The supplier renamed the items: loading names by code updates them and
	// keeps the columns the file does not have.
	renamed := file("Код;Наименование\nIT-1;Масло 5W-30 (4 л)\nIT-3;Новинка\n")
	status, updated := call("POST", "/catalog/import-items/import", body(map[string]any{"fileName": "items.csv", "content": renamed,
		"mapping": map[string]string{"Код": "code", "Наименование": "name"}, "matchByCode": true}))
	require.Equal(t, 200, status, updated)
	require.Equal(t, false, updated["applied"], "a new item needs its unit")
	require.Equal(t, float64(1), updated["updated"])
	require.Equal(t, "обязательное поле не заполнено", updated["errors"].([]any)[0].(map[string]any)["message"])

	status, missing := call("POST", "/catalog/import-items/import", body(map[string]any{"fileName": "items.csv", "content": renamed,
		"mapping": map[string]string{"Код": "code", "Наименование": "name"}}))
	require.Equal(t, 400, status, missing)
	require.Contains(t, fmt.Sprint(missing), "Сопоставьте колонку с обязательным полем «Единица»")
}
