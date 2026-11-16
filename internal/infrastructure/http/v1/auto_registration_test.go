package v1

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/http/contracts"
	_ "metapus/internal/infrastructure/http/jsoncodec"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/metadata"
	"metapus/internal/platform"
)

var autoProbeDefinition = entity.Definition{Kind: entity.CatalogKind, Key: "auto_probe", Name: "AutoProbe", Route: "auto-probes", Table: "cat_auto_probes"}

type autoProbe struct {
	entity.Catalog
	Plate     string    `db:"plate" json:"plate" meta:"label:Госномер" binding:"required"`
	Mileage   int       `db:"mileage" json:"mileage,omitempty"`
	CheckedAt time.Time `db:"checked_at" json:"checkedAt" meta:"readOnly:true"`
	Secret    string    `db:"secret" json:"-"`
	Cache     string    `json:"cache"`
}

func (autoProbe) EntityDefinition() entity.Definition { return autoProbeDefinition }

var autoProbeDocumentDefinition = entity.Definition{Kind: entity.DocumentKind, Key: "auto_waybill", Name: "AutoWaybill", Route: "auto-waybill", Table: "doc_auto_waybills"}

type autoWaybill struct {
	entity.Document
	Distance int `db:"distance" json:"distance"`
}

func (autoWaybill) EntityDefinition() entity.Definition { return autoProbeDocumentDefinition }
func (*autoWaybill) GetDocumentType() string            { return autoProbeDocumentDefinition.Name }

func jsonNames(t reflect.Type) []string {
	var names []string
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		names = append(names, name)
	}
	return names
}

func TestAutoShapeWritesOnlyDeclaredColumnsAndKeepsServerFieldsReadOnly(t *testing.T) {
	shape := autoShapeFor[autoProbe]()
	require.Equal(t, []string{"code", "name", "attributes", "plate", "mileage"}, jsonNames(shape.create))
	require.Equal(t, []string{"code", "name", "attributes", "plate", "mileage", "version"}, jsonNames(shape.update))
	require.Equal(t, []string{"id", "version", "deletionMark", "code", "name", "attributes", "plate", "mileage", "checkedAt"}, jsonNames(shape.read))

	plate, _ := shape.create.FieldByName("Plate")
	require.Equal(t, `json:"plate" binding:"required"`, string(plate.Tag), "only json and binding tags reach the wire")

	doc := autoShapeFor[autoWaybill]()
	require.Equal(t, []string{"number", "date", "description", "attributes", "distance", "postImmediately"}, jsonNames(doc.create))
	require.Equal(t, []string{"id", "version", "deletionMark", "posted", "postedVersion", "number", "date", "description", "attributes", "distance"}, jsonNames(doc.read))
}

func TestAutoRequestsAreStrictValidatedAndMappedOntoTheModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := handlers.NewBaseHandler()
	var created *autoProbe
	router := gin.New()
	router.POST("/", func(c *gin.Context) {
		var req AutoCreate[autoProbe]
		if !base.BindJSON(c, &req) {
			c.Status(http.StatusBadRequest)
			return
		}
		created = newAutoModel[autoProbe](req.value, entity.CatalogKind)
		autoShapeFor[autoProbe]().applyAutoRequest(req.value, created)
		c.Status(http.StatusCreated)
	})
	post := func(body string) int {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		return rec.Code
	}

	require.Equal(t, http.StatusBadRequest, post(`{"name":"A","plate":"X1","id":"01a1e528-8ff1-7e8a-9eaf-046da8ba2631"}`), "server-owned id is not writable")
	require.Equal(t, http.StatusBadRequest, post(`{"name":"A","plate":"X1","checkedAt":"2026-01-01T00:00:00Z"}`), "read-only field is not writable")
	require.Equal(t, http.StatusBadRequest, post(`{"name":"A","plate":"X1","secret":"s"}`), "json:\"-\" field is not on the wire")
	require.Equal(t, http.StatusBadRequest, post(`{"name":"A"}`), "binding:\"required\" of the model applies")
	require.Equal(t, http.StatusBadRequest, post(`{"plate":"X1"}`), "a catalog name is required")

	require.Equal(t, http.StatusCreated, post(`{"code":"C-1","name":"Газель","plate":"А001АА","mileage":12000}`))
	require.Equal(t, "C-1", created.Code)
	require.Equal(t, "Газель", created.Name)
	require.Equal(t, "А001АА", created.Plate)
	require.Equal(t, 12000, created.Mileage)
	require.False(t, id.IsNil(created.ID), "a new catalog item gets its identity from entity.NewCatalog")

	created.ID = id.New()
	created.Version = 3
	created.CheckedAt = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	created.Secret = "never"
	response := reflect.ValueOf(autoShapeFor[autoProbe]().response(created, nil, nil))
	require.Equal(t, created.ID.String(), response.FieldByName("ID").String())
	require.Equal(t, 3, int(response.FieldByName("Version").Int()))
	require.Equal(t, created.CheckedAt, response.FieldByName("CheckedAt").Interface())
	require.False(t, response.FieldByName("Secret").IsValid())
}

func TestAutoUpdateKeepsStoredAttributesWhenOmitted(t *testing.T) {
	shape := autoShapeFor[autoProbe]()
	stored := &autoProbe{Catalog: entity.NewCatalog("C-1", "Старое"), Plate: "P"}
	stored.Attributes = entity.Attributes{"color": "red"}
	request := reflect.New(shape.update).Elem()
	request.FieldByName("Name").SetString("Новое")
	request.FieldByName("Plate").SetString("P2")
	shape.applyAutoRequest(request, stored)
	require.Equal(t, "Новое", stored.Name)
	require.Equal(t, "P2", stored.Plate)
	require.Equal(t, entity.Attributes{"color": "red"}, stored.Attributes)
}

func TestAutoRegistrationsPublishDerivedContractsAndRoutes(t *testing.T) {
	catalog := Catalog[autoProbe](metadata.Presentation{Singular: "Проба", Plural: "Пробы"})
	document := Document[autoWaybill](metadata.Presentation{Singular: "Путевой лист", Plural: "Путевые листы"})
	reg := NewFactoryRegistry()
	reg.RegisterCatalog(catalog)
	reg.RegisterDocument(document)

	bundle := reg.HTTPContracts()
	output, err := contracts.Generate(bundle.Contracts)
	require.NoError(t, err)
	generated := string(output)
	for _, want := range []string{"export type CreateAutoProbeRequest", "export type UpdateAutoProbeRequest", "export type AutoProbeResponse", "export type AutoProbeCursorResponse", "export type CreateAutoWaybillRequest"} {
		require.Contains(t, generated, want)
	}
	create := generated[strings.Index(generated, "export type CreateAutoProbeRequest"):]
	create = create[:strings.Index(create, "}")]
	require.Contains(t, create, `"plate": string`)
	require.NotContains(t, create, "checkedAt")

	routes := map[string]bool{}
	for _, route := range bundle.Routes {
		routes[route.Method+" "+route.Path] = true
	}
	require.True(t, routes["POST /catalog/auto-probes"])
	require.True(t, routes["POST /document/auto-waybill/:id/post"])

	codes := map[string]bool{}
	for _, p := range reg.Permissions() {
		codes[p.Code] = true
	}
	require.True(t, codes["catalog:auto_probe:create"])
	require.True(t, codes["document:auto_waybill:unpost"])
	require.Equal(t, "AP", autoCodePrefix("auto_probe"))
	require.Equal(t, "VE", autoCodePrefix("vehicle"))
}

func TestAutoRegistrationRejectsModelsOutsideTheRule(t *testing.T) {
	_, err := deriveAutoShape(reflect.TypeFor[struct{ Plate string }](), autoProbeDefinition)
	require.ErrorContains(t, err, "embed entity.Catalog")
	type clash struct {
		entity.Catalog
		Title string `db:"title" json:"name"`
	}
	_, err = deriveAutoShape(reflect.TypeFor[clash](), autoProbeDefinition)
	require.ErrorContains(t, err, `json name "name"`)
}

var autoRefDefinition = entity.Definition{Kind: entity.CatalogKind, Key: "auto_route", Name: "AutoRoute", Route: "auto-routes", Table: "cat_auto_routes"}

type autoRoute struct {
	entity.Catalog
	VehicleID  id.ID  `db:"vehicle_id" json:"vehicleId" meta:"label:ТС,ref:carrier"`
	CurrencyID *id.ID `db:"currency_id" json:"currencyId,omitempty" meta:"ref:auto_probe"`
	OwnerID    id.ID  `db:"owner_id" json:"ownerId"`
}

func (autoRoute) EntityDefinition() entity.Definition { return autoRefDefinition }

func TestAutoReferencesResolveThroughTheRegistry(t *testing.T) {
	shape := autoShapeFor[autoRoute]()
	require.Equal(t, []string{"id", "version", "deletionMark", "code", "name", "attributes", "vehicleId", "currencyId", "ownerId", "vehicle", "currency"}, jsonNames(shape.read),
		"a ref-tagged id gets a display; an untagged id does not")

	reg := NewFactoryRegistry()
	probe := Catalog[autoProbe](metadata.Presentation{Plural: "Пробы"}).References("carrier", "auto_probe")
	route := Catalog[autoRoute](metadata.Presentation{Plural: "Маршруты"})
	reg.RegisterCatalog(probe)
	reg.RegisterCatalog(route)
	tables := route.referenceTables(shape)
	require.Equal(t, map[string]string{"carrier": "cat_auto_probes", "auto_probe": "cat_auto_probes"}, tables)

	vehicle, currency := id.New(), id.New()
	model := &autoRoute{Catalog: entity.NewCatalog("R-1", "Маршрут"), VehicleID: vehicle, CurrencyID: &currency}
	resolved := postgres.ResolvedRefs{"cat_auto_probes": {vehicle.String(): {ID: vehicle.String(), Name: "Газель", Code: "V-1"}}}
	out := reflect.ValueOf(shape.response(model, tables, resolved))
	require.Equal(t, "Газель", out.FieldByName("VehicleRef").Interface().(*postgres.RefDisplay).Name)
	require.True(t, out.FieldByName("CurrencyRef").IsNil(), "an unresolved reference stays absent")

	lonely := NewFactoryRegistry()
	orphan := Catalog[autoRoute](metadata.Presentation{Plural: "Маршруты"})
	lonely.RegisterCatalog(orphan)
	require.Panics(t, func() { orphan.referenceTables(shape) }, "a reference to an unregistered key is a declaration error")
}

func TestAutoCatalogOptionsShapeItsRegistration(t *testing.T) {
	built := false
	reg := Catalog[autoProbe](metadata.Presentation{Plural: "Пробы"}).
		References("supplier", "customer").
		Search(platform.SearchFields{SearchCols: []string{"name", "plate"}, TitleCol: "name"}).
		Service(func(deps CatalogDeps) *domain.CatalogService[*autoProbe] {
			built = true
			def := autoProbeDefinition
			return domain.NewCatalogService(domain.CatalogServiceConfig[*autoProbe]{Definition: &def, Repo: catalog_repo.NewCatalogRepoForDefinition(def, postgres.ExtractDBColumns[autoProbe](), func() *autoProbe { return new(autoProbe) })})
		})
	require.Equal(t, []string{"supplier", "customer"}, reg.ReferenceTypes())
	require.Equal(t, "Пробы", reg.EntityLabel())
	fields, ok := searchableFields(reg)
	require.True(t, ok)
	require.Equal(t, []string{"name", "plate"}, fields.SearchCols)
	_, ok = searchableFields(Catalog[autoProbe](metadata.Presentation{}))
	require.False(t, ok, "a catalog without Search keeps the default name/code search")
	reg.Build(CatalogDeps{BaseHandler: handlers.NewBaseHandler()})
	require.True(t, built, "Build uses the entity's own service")
}
