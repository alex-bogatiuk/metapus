//go:build integration

package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"metapus/internal/application/readaccess"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
	_ "metapus/internal/infrastructure/http/jsoncodec"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
	"metapus/internal/metadata"
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
		provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "configuration", fixture.name)), goose.WithAllowOutofOrder(true))
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
var declaredCatalogDefinition = entity.Definition{Kind: entity.CatalogKind, Key: "fleet_vehicle", Name: "FleetVehicle", Route: "fleet-vehicles", Table: "cat_fleet_vehicles"}

type declaredVehicle struct {
	entity.Catalog
	Plate    string `db:"plate" json:"plate" binding:"required"`
	Mileage  int    `db:"mileage" json:"mileage"`
	Verified bool   `db:"verified" json:"verified" meta:"readOnly:true"`
}

func (declaredVehicle) EntityDefinition() entity.Definition { return declaredCatalogDefinition }

var declaredDocumentDefinition = entity.Definition{Kind: entity.DocumentKind, Key: "fleet_waybill", Name: "FleetWaybill", Route: "fleet-waybill", Table: "doc_fleet_waybills"}

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
		provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "configuration", fixture.name)), goose.WithAllowOutofOrder(true))
		require.NoError(t, err)
		_, err = provider.Up(ctx)
		require.NoError(t, err)
	}
	_, err = tenant.Pool.Exec(ctx, `ALTER TABLE cat_fleet_vehicles ADD COLUMN plate TEXT NOT NULL DEFAULT '', ADD COLUMN mileage INT NOT NULL DEFAULT 0, ADD COLUMN verified BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE doc_fleet_waybills ADD COLUMN distance INT NOT NULL DEFAULT 0`)
	require.NoError(t, err)

	reg := v1.NewFactoryRegistry()
	reg.RegisterCatalog(v1.Catalog[declaredVehicle](metadata.Presentation{Singular: "ТС", Plural: "Транспорт"}).CodePrefix("FV"))
	reg.RegisterDocument(v1.Document[declaredWaybill](metadata.Presentation{Singular: "Путевой лист", Plural: "Путевые листы"}).NumberPrefix("FW"))
	metaReg := metadata.NewRegistry()
	for _, entry := range []struct {
		model any
		def   entity.Definition
		kind  metadata.EntityType
	}{{declaredVehicle{}, declaredCatalogDefinition, metadata.TypeCatalog}, {declaredWaybill{}, declaredDocumentDefinition, metadata.TypeDocument}} {
		def := metadata.Inspect(entry.model, entry.def.Name, entry.kind)
		def.Key, def.TableName, def.ReadPermission = entry.def.Key, entry.def.Table, entry.def.Permission()+":read"
		metaReg.Register(def)
	}
	engine, err := security.NewPolicyEngine()
	require.NoError(t, err)
	defer engine.Stop()
	reads := postgres.NewAuthorizedReadService(metaReg, engine)
	principal := appctx.UserContext{UserID: id.New().String(), IsAdmin: true, Roles: []string{"admin"}}
	_, err = tenant.Pool.Exec(ctx, `INSERT INTO auth_users(id,email,password_hash) VALUES($1,'declared@test','fixture')`, principal.UserID)
	require.NoError(t, err)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(readaccess.WithService(tenant.Context(principal), reads))
		c.Next()
	})
	router.Use(middleware.ErrorHandler())
	base := handlers.NewBaseHandler()
	catalog := reg.Catalogs()[0]
	v1.RegisterCatalogRoutes(router.Group("/catalog/"+catalog.RoutePrefix()), catalog.Build(v1.CatalogDeps{BaseHandler: base, Numerator: numerator.Noop()}), catalog.Permission())
	document := reg.Documents()[0]
	v1.RegisterDocumentRoutes(router.Group("/document/"+document.RoutePrefix()), document.Build(v1.DocumentDeps{BaseHandler: base, Numerator: numerator.Noop(), PostingEngine: posting.NewEngine(nil)}),
		document.Permission(), document.(v1.DocumentCapabilitiesProvider).DocumentCapabilities())

	call := func(method, path, body string) (int, map[string]any) {
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
