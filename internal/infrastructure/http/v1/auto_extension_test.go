package v1

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

var extensionPartDefinition = entity.Definition{Kind: entity.CatalogKind, Key: "ext_part", Name: "ExtPart", Route: "ext-parts", Table: "cat_ext_parts"}

type extPart struct {
	entity.Catalog
	Article string `db:"article" json:"article" meta:"label:Артикул"`
}

func (extPart) EntityDefinition() entity.Definition { return extensionPartDefinition }

type extPartShop struct {
	Brand      string `db:"brand" json:"brand" meta:"label:Бренд"`
	Original   bool   `db:"original" json:"original" meta:"label:Оригинал"`
	Normalized string `db:"normalized" json:"normalized" meta:"label:Нормализованный,readOnly:true"`
}

func TestExtendRefusesFieldsThePlatformCannotServe(t *testing.T) {
	t.Parallel()
	def := extensionPartDefinition
	cases := map[string]struct {
		owner string
		model any
		want  string
	}{
		"owner":      {"Auto Shop", extPartShop{}, "lowercase identifier"},
		"embedded":   {"shop", struct{ extPartShop }{}, "embedded field"},
		"no column":  {"shop", struct{ Brand string `json:"brand"` }{}, "needs a db column"},
		"snake case": {"shop", struct{ Brand string `db:"brand_name" json:"brand"` }{}, "snake case"},
		"id":         {"shop", struct{ ID string `db:"id" json:"id"` }{}, "item's id"},
		"reference":  {"shop", struct{ BrandID id.ID `db:"brand_id" json:"brandId" meta:"ref:brand"` }{}, "references in extensions"},
		"calculated": {"shop", struct{ Total int `db:"total" json:"total" meta:"calc:a*b"` }{}, "OnWrite"},
		"no fields":  {"shop", struct{}{}, "declares no fields"},
	}
	for name, tc := range cases {
		_, err := deriveExtensionSpec(tc.owner, "Магазин", def, reflect.TypeOf(tc.model))
		require.ErrorContains(t, err, tc.want, name)
	}
	document := def
	document.Kind = entity.DocumentKind
	_, err := deriveExtensionSpec("shop", "Магазин", document, reflect.TypeFor[extPartShop]())
	require.ErrorContains(t, err, "not a catalog")
}

func TestExtensionIsNamedAfterItsCatalogAndAddOn(t *testing.T) {
	t.Parallel()
	ext := Extend[extPart, extPartShop]("auto_shop", "Магазин")
	require.Equal(t, "cat_ext_parts__auto_shop", ext.Table())
	bundle := ext.HTTPContracts()
	require.Equal(t, "ExtPartAutoShopRequest", bundle.Contracts[0].Name)
	require.Equal(t, "ExtPartAutoShopResponse", bundle.Contracts[1].Name)
	request := bundle.Contracts[0].Type
	require.Equal(t, 2, request.NumField(), "the read-only field is not writable")
	brand, _ := request.FieldByName("Brand")
	require.Equal(t, reflect.TypeFor[*string](), brand.Type, "an omitted field keeps its value")
	var routes []string
	for _, route := range bundle.Routes {
		require.True(t, route.Extends)
		routes = append(routes, route.Method+" "+route.Path)
	}
	require.Equal(t, []string{"GET /catalog/ext-parts/:id", "POST /catalog/ext-parts", "PUT /catalog/ext-parts/:id"}, routes)
}

func TestExtendedShapeCarriesTheExtensionBesideTheCatalog(t *testing.T) {
	t.Parallel()
	reg := NewFactoryRegistry()
	catalog := Catalog[extPart](metadata.Presentation{Singular: "Деталь", Plural: "Детали"})
	reg.RegisterCatalog(catalog)
	reg.RegisterExtension(Extend[extPart, extPartShop]("shop", "Магазин"))
	shape := extendAutoShape(autoShapeFor[extPart](), catalog.extensionSpecs())

	request := AutoCreate[extPart]{shape: shape}
	require.NoError(t, json.Unmarshal([]byte(`{"name":"Фильтр","article":"OC-90","brand":"MAHLE","original":true}`), &request))
	model := newAutoModel[extPart](request.value, entity.CatalogKind)
	shape.applyAutoRequest(request.value, model)
	ext := entity.ExtensionOf[extPartShop](model)
	require.Equal(t, extPartShop{Brand: "MAHLE", Original: true}, *ext)

	update := AutoUpdate[extPart]{shape: shape}
	require.NoError(t, json.Unmarshal([]byte(`{"name":"Фильтр","article":"OC-90","original":false,"version":1}`), &update))
	shape.applyAutoRequest(update.value, model)
	require.Equal(t, extPartShop{Brand: "MAHLE"}, *ext, "an omitted field keeps its value, a sent one replaces it")

	strict := AutoCreate[extPart]{shape: shape}
	require.Error(t, json.UnmarshalStrict([]byte(`{"name":"Фильтр","normalized":"X"}`), &strict), "a read-only field is not writable")
	base := AutoCreate[extPart]{}
	require.Error(t, json.UnmarshalStrict([]byte(`{"name":"Фильтр","brand":"MAHLE"}`), &base), "the catalog alone does not know the extension")

	ext.Normalized = "OC90"
	out, err := json.Marshal(shape.response(model, nil, nil))
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))
	require.Equal(t, "OC-90", body["article"])
	require.Equal(t, "MAHLE", body["brand"])
	require.Equal(t, "OC90", body["normalized"])

	bare, err := json.Marshal(shape.response(&extPart{Catalog: entity.NewCatalog("", "Без расширения")}, nil, nil))
	require.NoError(t, err)
	require.Contains(t, string(bare), `"brand":""`, "an item stored before the add-on reads as the zero extension")
}

func TestExtensionFieldsJoinTheCatalogMetadataInTheAddOnGroup(t *testing.T) {
	t.Parallel()
	reg := NewFactoryRegistry()
	catalog := Catalog[extPart](metadata.Presentation{Singular: "Деталь", Plural: "Детали"})
	reg.RegisterExtension(Extend[extPart, extPartShop]("shop", "Магазин"))
	reg.RegisterCatalog(catalog) // the extension may come first
	def := metadata.Inspect(extPart{}, "ExtPart", metadata.TypeCatalog)
	appendExtensionMetadata(&def, catalog)
	fields := map[string]metadata.FieldDef{}
	for _, f := range def.Fields {
		fields[f.Name] = f
	}
	require.Equal(t, "article", fields["article"].DBColumn)
	brand := fields["brand"]
	require.Equal(t, "Бренд", brand.Label)
	require.Equal(t, "Магазин", brand.Group)
	require.Empty(t, brand.DBColumn, "the catalog's table has no such column: reports and authorized reads skip it")
	require.True(t, fields["normalized"].ReadOnly)
}

func TestRegistryRefusesConflictingExtensions(t *testing.T) {
	t.Parallel()
	require.PanicsWithValue(t, "duplicate extension shop of ext_part", func() {
		reg := NewFactoryRegistry()
		reg.RegisterExtension(Extend[extPart, extPartShop]("shop", "Магазин"))
		reg.RegisterExtension(Extend[extPart, extPartShop]("shop", "Магазин"))
	})
	require.Panics(t, func() {
		reg := NewFactoryRegistry()
		reg.RegisterExtension(Extend[extPart, extPartShop]("shop", "Магазин"))
		reg.RegisterExtension(Extend[extPart, struct {
			Brand string `db:"brand" json:"brand"`
		}]("fleet", "Автопарк"))
	}, "two add-ons may not declare one field")

	reg := NewFactoryRegistry()
	catalog := Catalog[extPart](metadata.Presentation{Singular: "Деталь", Plural: "Детали"})
	reg.RegisterCatalog(catalog)
	reg.RegisterExtension(Extend[extPart, struct {
		Article string `db:"article" json:"article"`
	}]("shop", "Магазин"))
	require.PanicsWithValue(t, `extension shop of ext_part: json name "article" is already a field of the catalog`, func() { catalog.extensionSpecs() })

	orphan := NewFactoryRegistry()
	orphan.RegisterExtension(Extend[extPart, extPartShop]("shop", "Магазин"))
	require.PanicsWithValue(t, "extension shop of ext_part: the catalog is not registered", orphan.checkExtensionTargets)
}

func TestExtensionFieldsFollowTheFieldPolicies(t *testing.T) {
	t.Parallel()
	s, err := deriveExtensionSpec("shop", "Магазин", extensionPartDefinition, reflect.TypeFor[extPartShop]())
	require.NoError(t, err)
	ctx := security.WithFieldPolicies(context.Background(), map[string]*security.FieldPolicy{
		"ext_part:read":  {AllowedFields: []string{"*", "-original"}},
		"ext_part:write": {AllowedFields: []string{"*", "-brand", "-original"}},
	})
	edited := func(value extPartShop) reflect.Value { return reflect.ValueOf(&value).Elem() }
	stored := edited(extPartShop{Brand: "MANN", Original: true})

	forbidden := func(err error) bool { return apperror.IsAppError(err) && strings.Contains(err.Error(), "read-only") }
	require.True(t, forbidden(checkExtensionWrite(ctx, "ext_part", s, edited(extPartShop{Brand: "MANN"}), reflect.Value{})), "a new item may not fill a field the editor may not write")
	require.NoError(t, checkExtensionWrite(ctx, "ext_part", s, edited(extPartShop{Normalized: "X"}), reflect.Value{}), "server-owned fields are the hook's")

	value := edited(extPartShop{Brand: "MANN"})
	require.NoError(t, checkExtensionWrite(ctx, "ext_part", s, value, stored))
	require.True(t, value.FieldByName("Original").Bool(), "a hidden field the masked form sent back empty keeps its stored value")
	require.True(t, forbidden(checkExtensionWrite(ctx, "ext_part", s, edited(extPartShop{Brand: "BOSCH", Original: true}), stored)), "a readable field the editor may not write keeps its value")

	item := &extPart{Catalog: entity.NewCatalog("", "Фильтр")}
	*entity.ExtensionOf[extPartShop](item) = extPartShop{Brand: "MANN", Original: true}
	maskExtensions(ctx, "ext_part", []*extensionSpec{s}, []*extPart{item})
	require.Equal(t, extPartShop{Brand: "MANN"}, *entity.ExtensionOf[extPartShop](item), "the read projection hides what the read policy hides")
}
