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

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/contracts"
	_ "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/jsoncodec"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/handlers"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
	"github.com/alex-bogatiuk/metapus/internal/platform"
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
	require.Equal(t, []string{"number", "date", "description", "basisType", "basisId", "attributes", "distance", "postImmediately"}, jsonNames(doc.create))
	require.Equal(t, []string{"id", "version", "deletionMark", "posted", "postedVersion", "number", "date", "description", "basisType", "basisId", "attributes", "distance"}, jsonNames(doc.read))
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

var autoReceiptDefinition = entity.Definition{
	Kind: entity.DocumentKind, Key: "auto_receipt", Name: "AutoReceipt", Route: "auto-receipts", Table: "doc_auto_receipts",
	Parts: []entity.Part{{Key: "goods", Field: "items"}, {Key: "services", Field: "services"}},
}

type autoReceiptItem struct {
	LineID    id.ID  `db:"line_id" json:"lineId"`
	LineNo    int    `db:"line_no" json:"lineNo"`
	ProductID id.ID  `db:"product_id" json:"productId" meta:"ref:auto_probe" binding:"required"`
	Quantity  int    `db:"quantity" json:"quantity" binding:"min=1"`
	Batch     string `db:"batch" json:"batch,omitempty" meta:"readOnly:true"`
}

type autoReceiptService struct {
	LineID id.ID  `db:"line_id" json:"lineId"`
	LineNo int    `db:"line_no" json:"lineNo"`
	Title  string `db:"title" json:"title" binding:"required"`
}

type autoReceipt struct {
	entity.Document
	Warehouse string               `db:"warehouse" json:"warehouse"`
	Items     []autoReceiptItem    `db:"-" json:"items" meta:"label:Товары"`
	Services  []autoReceiptService `db:"-" json:"services" meta:"label:Услуги"`
}

func (autoReceipt) EntityDefinition() entity.Definition { return autoReceiptDefinition }
func (*autoReceipt) GetDocumentType() string            { return autoReceiptDefinition.Name }

func TestAutoDocumentPartsAreOnTheWireWithTheirLineRules(t *testing.T) {
	shape := autoShapeFor[autoReceipt]()
	require.Equal(t, []string{"number", "date", "description", "basisType", "basisId", "attributes", "warehouse", "items", "services", "postImmediately"}, jsonNames(shape.create))
	require.Equal(t, []string{"id", "version", "deletionMark", "posted", "postedVersion", "number", "date", "description", "basisType", "basisId", "attributes", "warehouse", "items", "services"}, jsonNames(shape.read))
	require.Len(t, shape.parts, 2)
	items := shape.parts[0]
	require.Equal(t, []string{"lineId", "productId", "quantity"}, jsonNames(items.create), "lineNo and read-only columns are not writable")
	require.Equal(t, []string{"lineId", "lineNo", "productId", "quantity", "batch", "product"}, jsonNames(items.read))

	gin.SetMode(gin.TestMode)
	base := handlers.NewBaseHandler()
	router := gin.New()
	router.POST("/", func(c *gin.Context) {
		var req AutoCreate[autoReceipt]
		if !base.BindJSON(c, &req) {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusCreated)
	})
	post := func(body string) int {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		return rec.Code
	}
	product := id.New().String()
	require.Equal(t, http.StatusCreated, post(`{"date":"2026-11-17T00:00:00Z","items":[{"productId":"`+product+`","quantity":2}]}`))
	require.Equal(t, http.StatusBadRequest, post(`{"date":"2026-11-17T00:00:00Z","items":[{"productId":"`+product+`","quantity":0}]}`), "line binding rules apply to every line")
	require.Equal(t, http.StatusBadRequest, post(`{"date":"2026-11-17T00:00:00Z","services":[{}]}`), "a required line field is required")
	require.Equal(t, http.StatusBadRequest, post(`{"date":"2026-11-17T00:00:00Z","items":[{"productId":"`+product+`","quantity":1,"batch":"B"}]}`), "a read-only line field is not writable")
	require.Equal(t, http.StatusBadRequest, post(`{"date":"2026-11-17T00:00:00Z","items":[{"productId":"`+product+`","quantity":1,"lineNo":5}]}`), "line numbers are the server's")
}

func TestAutoDocumentPartsKeepKnownLineIDsAndRenumber(t *testing.T) {
	shape := autoShapeFor[autoReceipt]()
	kept, foreign := id.New(), id.New()
	stored := &autoReceipt{Document: entity.NewDocument(),
		Items:    []autoReceiptItem{{LineID: kept, LineNo: 1, ProductID: id.New(), Quantity: 1, Batch: "B-1"}},
		Services: []autoReceiptService{{LineID: id.New(), LineNo: 1, Title: "Доставка"}},
	}
	request := reflect.New(shape.update).Elem()
	items := reflect.MakeSlice(reflect.SliceOf(shape.parts[0].create), 3, 3)
	for i, lineID := range []*id.ID{&foreign, &kept, &kept} {
		items.Index(i).FieldByName("LineID").Set(reflect.ValueOf(lineID))
		items.Index(i).FieldByName("ProductID").Set(reflect.ValueOf(id.New()))
		items.Index(i).FieldByName("Quantity").SetInt(int64(i + 1))
	}
	request.FieldByName("Items").Set(items)
	shape.applyAutoRequest(request, stored)

	require.Len(t, stored.Items, 3)
	require.NotEqual(t, foreign, stored.Items[0].LineID, "an id of no stored line of this document is replaced")
	require.Equal(t, kept, stored.Items[1].LineID, "a stored line keeps its id")
	require.NotEqual(t, kept, stored.Items[2].LineID, "an id already taken by an earlier line is replaced")
	require.Equal(t, []int{1, 2, 3}, []int{stored.Items[0].LineNo, stored.Items[1].LineNo, stored.Items[2].LineNo})
	require.Equal(t, "B-1", stored.Items[1].Batch, "a kept line keeps its read-only fields")
	require.Empty(t, stored.Items[0].Batch, "a new line starts without them")
	require.Equal(t, 2, stored.Items[1].Quantity, "a kept line takes the requested writable fields")
	require.Equal(t, "Доставка", stored.Services[0].Title, "an omitted part keeps its stored lines")

	request.FieldByName("Services").Set(reflect.MakeSlice(reflect.SliceOf(shape.parts[1].create), 0, 0))
	shape.applyAutoRequest(request, stored)
	require.Empty(t, stored.Services, "an empty array clears the part")

	created := newAutoModel[autoReceipt](reflect.New(shape.create).Elem(), entity.DocumentKind)
	shape.applyAutoRequest(reflect.New(shape.create).Elem(), created)
	require.NotNil(t, created.Items, "a new document starts with empty parts")
}

func TestAutoDocumentPartsResolveLineReferencesAndPublishLineContracts(t *testing.T) {
	reg := NewFactoryRegistry()
	reg.RegisterCatalog(Catalog[autoProbe](metadata.Presentation{Plural: "Пробы"}))
	receipt := Document[autoReceipt](metadata.Presentation{Singular: "Поступление", Plural: "Поступления"})
	reg.RegisterDocument(receipt)
	shape := autoShapeFor[autoReceipt]()
	tables := receipt.referenceTables(shape)
	require.Equal(t, map[string]string{"auto_probe": "cat_auto_probes"}, tables)

	product := id.New()
	model := &autoReceipt{Document: entity.NewDocument(), Items: []autoReceiptItem{{LineID: id.New(), LineNo: 1, ProductID: product, Quantity: 4}}}
	resolver := postgres.NewReferenceResolver()
	shape.collectRefs(resolver, tables, model)
	resolved := postgres.ResolvedRefs{"cat_auto_probes": {product.String(): {ID: product.String(), Name: "Гвозди"}}}
	out := reflect.ValueOf(shape.response(model, tables, resolved))
	line := out.FieldByName("Items").Index(0)
	require.Equal(t, "Гвозди", line.FieldByName("ProductRef").Interface().(*postgres.RefDisplay).Name)
	require.Equal(t, 1, int(line.FieldByName("LineNo").Int()))
	require.True(t, out.FieldByName("Services").IsNil(), "a part that was not loaded stays off the response")

	encoded, err := json.Marshal(shape.response(&autoReceipt{Document: entity.NewDocument()}, tables, nil))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"items"`, "list rows carry no lines")

	parts := map[string]string{}
	for _, def := range BuildReadRegistry(reg).List() {
		for _, tp := range def.TableParts {
			parts[def.Name+"."+tp.Name] = tp.SQLTable
		}
	}
	require.Equal(t, "doc_auto_receipts__goods", parts["AutoReceipt.items"], "filters and FLS reach the part through its SQL table")
	require.Equal(t, "doc_auto_receipts__services", parts["AutoReceipt.services"])

	output, err := contracts.Generate(reg.HTTPContracts().Contracts)
	require.NoError(t, err)
	generated := string(output)
	require.Contains(t, generated, "export type AutoReceiptItemsLineRequest")
	require.Contains(t, generated, "export type AutoReceiptServicesLineResponse")
	create := generated[strings.Index(generated, "export type CreateAutoReceiptRequest"):]
	create = create[:strings.Index(create, "}")]
	require.Contains(t, create, `"items"?: Array<AutoReceiptItemsLineRequest>`)
}

func TestAutoDocumentPartsRejectUndeclaredLines(t *testing.T) {
	type noLineNo struct {
		LineID id.ID  `db:"line_id" json:"lineId"`
		Title  string `db:"title" json:"title"`
	}
	type broken struct {
		entity.Document
		Services []noLineNo `db:"-" json:"services"`
	}
	def := entity.Definition{Kind: entity.DocumentKind, Key: "auto_broken", Name: "AutoBroken", Route: "auto-broken", Table: "doc_auto_broken",
		Parts: []entity.Part{{Key: "services", Field: "services"}}}
	_, err := deriveAutoShape(reflect.TypeFor[broken](), def)
	require.ErrorContains(t, err, "line_id and line_no")

	missing := def
	missing.Parts = []entity.Part{{Key: "goods", Field: "goods"}}
	_, err = deriveAutoShape(reflect.TypeFor[broken](), missing)
	require.ErrorContains(t, err, `no field with json name "goods"`)

	catalog := autoProbeDefinition
	catalog.Parts = []entity.Part{{Key: "goods", Field: "goods"}}
	_, err = deriveAutoShape(reflect.TypeFor[autoProbe](), catalog)
	require.ErrorContains(t, err, "documents only")
}
