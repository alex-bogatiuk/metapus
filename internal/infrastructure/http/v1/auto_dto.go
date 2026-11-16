package v1

import (
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin/binding"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/infrastructure/storage/postgres"
)

// autoShape is the wire projection of a declared entity, derived from its
// model by one rule instead of hand-written DTOs:
//
//   - writable: the kind's standard fields (catalog: code, name, and parentId
//     and isFolder when hierarchical; document: number, date, description),
//     attributes, and every exported field declared on the model itself that
//     has a db column and a json name and is not meta:"readOnly:true";
//   - create request: the writable fields (documents add postImmediately);
//   - update request: the writable fields and the expected version;
//   - response: id, version, deletionMark (documents add posted and
//     postedVersion), the writable fields and the model's read-only fields;
//     a reference field (id.ID or *id.ID tagged meta:"ref:<entity key>" whose
//     json name ends in "Id") also gets its resolved display under the name
//     without "Id", e.g. counterpartyId → counterparty: {id, name, code}.
//
// Server-owned base fields (id, version, posting state, timestamps) are never
// writable, and a model field without a db column is never on the wire. Only
// the json tag (name and options such as omitempty) and the binding tag are
// carried over, so validation rules such as binding:"required" apply to the
// request as declared on the model.
type autoShape struct {
	create, update, read reflect.Type
	writable             []string  // Go field names copied from a request onto the model
	readable             []string  // Go field names copied from the model onto the response
	refs                 []autoRef // reference fields resolved into the response
}

// autoRef is a reference field of the model and its display in the response.
type autoRef struct {
	field   string // model field holding the id.ID or *id.ID
	display string // response field holding *postgres.RefDisplay
	key     string // referenced entity key (meta ref)
}

var autoShapes sync.Map // reflect.Type (model struct) → *autoShape

// autoShapeFor returns the shape of model T, deriving it once.
func autoShapeFor[T entity.DefinitionProvider]() *autoShape {
	model := reflect.TypeFor[T]()
	if cached, ok := autoShapes.Load(model); ok {
		return cached.(*autoShape)
	}
	var zero T
	shape, err := deriveAutoShape(model, zero.EntityDefinition())
	if err != nil {
		panic(fmt.Sprintf("declarative registration of %v: %v", model, err))
	}
	actual, _ := autoShapes.LoadOrStore(model, shape)
	return actual.(*autoShape)
}

type autoField struct {
	name, json, binding string
}

func deriveAutoShape(model reflect.Type, def entity.Definition) (*autoShape, error) {
	if model.Kind() != reflect.Struct {
		return nil, fmt.Errorf("model must be a struct")
	}
	var base []autoField
	var serverRead []autoField
	switch def.Kind {
	case entity.CatalogKind:
		if !embeds(model, reflect.TypeFor[entity.Catalog]()) {
			return nil, fmt.Errorf("a catalog model must embed entity.Catalog")
		}
		base = []autoField{{"Code", "code", ""}, {"Name", "name", "required"}}
		if def.Hierarchy.Hierarchical {
			base = append(base, autoField{"ParentID", "parentId", ""}, autoField{"IsFolder", "isFolder", ""})
		}
	case entity.DocumentKind:
		if !embeds(model, reflect.TypeFor[entity.Document]()) {
			return nil, fmt.Errorf("a document model must embed entity.Document")
		}
		base = []autoField{{"Number", "number", ""}, {"Date", "date", "required"}, {"Description", "description", ""}}
		serverRead = []autoField{{"Posted", "posted", ""}, {"PostedVersion", "postedVersion", ""}}
	default:
		return nil, fmt.Errorf("unsupported kind %q", def.Kind)
	}
	base = append(base, autoField{"Attributes", "attributes", ""})

	shape := &autoShape{}
	var writable, readOnly []reflect.StructField
	names := map[string]string{}
	add := func(list *[]reflect.StructField, f reflect.StructField, jsonName, bindingTag string) error {
		if other, taken := names[jsonName]; taken {
			return fmt.Errorf("json name %q of %s is already used by %s", jsonName, f.Name, other)
		}
		names[jsonName] = f.Name
		// The model's json options (omitempty, …) carry over with the name.
		tag := `json:"` + jsonName
		if _, options, ok := strings.Cut(f.Tag.Get("json"), ","); ok {
			tag += "," + options
		}
		tag += `"`
		if bindingTag != "" {
			tag += ` binding:"` + bindingTag + `"`
		}
		*list = append(*list, reflect.StructField{Name: f.Name, Type: f.Type, Tag: reflect.StructTag(tag)})
		return nil
	}
	for _, b := range base {
		f, ok := model.FieldByName(b.name)
		if !ok {
			return nil, fmt.Errorf("standard field %s is missing", b.name)
		}
		if err := add(&writable, f, b.json, b.binding); err != nil {
			return nil, err
		}
	}
	for i := 0; i < model.NumField(); i++ {
		f := model.Field(i)
		if f.Anonymous || !f.IsExported() {
			continue
		}
		jsonName, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		column, _, _ := strings.Cut(f.Tag.Get("db"), ",")
		if jsonName == "" || jsonName == "-" || column == "" || column == "-" {
			continue
		}
		list := &writable
		if autoReadOnly(f) {
			list = &readOnly
		}
		if err := add(list, f, jsonName, f.Tag.Get("binding")); err != nil {
			return nil, err
		}
	}
	for _, f := range writable {
		shape.writable = append(shape.writable, f.Name)
	}

	create := append([]reflect.StructField(nil), writable...)
	if def.Kind == entity.DocumentKind {
		create = append(create, reflect.StructField{Name: "PostImmediately", Type: reflect.TypeFor[bool](), Tag: `json:"postImmediately"`})
	}
	update := append(append([]reflect.StructField(nil), writable...),
		reflect.StructField{Name: "Version", Type: reflect.TypeFor[int](), Tag: `json:"version" binding:"required,min=1"`})

	read := []reflect.StructField{
		{Name: "ID", Type: reflect.TypeFor[string](), Tag: `json:"id"`},
		{Name: "Version", Type: reflect.TypeFor[int](), Tag: `json:"version"`},
		{Name: "DeletionMark", Type: reflect.TypeFor[bool](), Tag: `json:"deletionMark"`},
	}
	for _, s := range serverRead {
		f, _ := model.FieldByName(s.name)
		read = append(read, reflect.StructField{Name: f.Name, Type: f.Type, Tag: reflect.StructTag(`json:"` + s.json + `"`)})
	}
	read = append(append(read, writable...), readOnly...)
	for _, f := range read[1:] {
		shape.readable = append(shape.readable, f.Name)
	}
	for _, f := range append(append([]reflect.StructField(nil), writable...), readOnly...) {
		original, _ := model.FieldByName(f.Name)
		key := autoRefKey(original)
		jsonName, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if key == "" || (f.Type != reflect.TypeFor[id.ID]() && f.Type != reflect.TypeFor[*id.ID]()) || !strings.HasSuffix(jsonName, "Id") {
			continue
		}
		displayJSON := strings.TrimSuffix(jsonName, "Id")
		if other, taken := names[displayJSON]; taken {
			return nil, fmt.Errorf("reference display %q of %s is already used by %s", displayJSON, f.Name, other)
		}
		names[displayJSON] = f.Name
		display := strings.TrimSuffix(f.Name, "ID") + "Ref"
		read = append(read, reflect.StructField{Name: display, Type: reflect.TypeFor[*postgres.RefDisplay](), Tag: reflect.StructTag(`json:"` + displayJSON + `,omitempty"`)})
		shape.refs = append(shape.refs, autoRef{field: f.Name, display: display, key: key})
	}

	shape.create, shape.update, shape.read = reflect.StructOf(create), reflect.StructOf(update), reflect.StructOf(read)
	return shape, nil
}

func embeds(model, base reflect.Type) bool {
	for i := 0; i < model.NumField(); i++ {
		if f := model.Field(i); f.Anonymous && f.Type == base {
			return true
		}
	}
	return false
}

// autoRefKey is the entity key of meta:"ref:<key>", or "".
func autoRefKey(f reflect.StructField) string {
	for part := range strings.SplitSeq(f.Tag.Get("meta"), ",") {
		if key, ok := strings.CutPrefix(strings.TrimSpace(part), "ref:"); ok {
			return key
		}
	}
	return ""
}

func autoReadOnly(f reflect.StructField) bool {
	for part := range strings.SplitSeq(f.Tag.Get("meta"), ",") {
		if strings.TrimSpace(part) == "readOnly:true" {
			return true
		}
	}
	return false
}

// newAutoModel allocates a model with the kind's initial state: a new catalog
// item with the requested code and name, or a new draft document.
func newAutoModel[T any](request reflect.Value, kind entity.Kind) *T {
	model := new(T)
	v := reflect.ValueOf(model).Elem()
	switch kind {
	case entity.CatalogKind:
		v.FieldByName("Catalog").Set(reflect.ValueOf(entity.NewCatalog(
			request.FieldByName("Code").String(), request.FieldByName("Name").String())))
	case entity.DocumentKind:
		v.FieldByName("Document").Set(reflect.ValueOf(entity.NewDocument()))
	}
	return model
}

// applyAutoRequest copies the writable fields of a decoded request onto the
// model. An omitted attributes object keeps the stored one.
func (s *autoShape) applyAutoRequest(request reflect.Value, model any) {
	target := reflect.ValueOf(model).Elem()
	for _, name := range s.writable {
		value := request.FieldByName(name)
		if name == "Attributes" && value.IsNil() {
			continue
		}
		target.FieldByName(name).Set(value)
	}
}

// response projects the model onto the response type. tables maps each
// reference key to its table; refs are the displays resolved for the batch,
// nil when the entity has no references.
func (s *autoShape) response(model any, tables map[string]string, refs postgres.ResolvedRefs) any {
	source := reflect.ValueOf(model).Elem()
	out := reflect.New(s.read).Elem()
	out.FieldByName("ID").SetString(fmt.Sprint(source.FieldByName("ID").Interface()))
	for _, name := range s.readable {
		out.FieldByName(name).Set(source.FieldByName(name))
	}
	for _, ref := range s.refs {
		if refs == nil {
			break
		}
		if target, ok := autoRefID(source.FieldByName(ref.field)); ok {
			if display := refs.Get(tables[ref.key], target); display.ID != "" {
				out.FieldByName(ref.display).Set(reflect.ValueOf(&display))
			}
		}
	}
	return out.Interface()
}

// collectRefs registers the model's references with the resolver.
func (s *autoShape) collectRefs(resolver *postgres.ReferenceResolver, tables map[string]string, model any) {
	source := reflect.ValueOf(model).Elem()
	for _, ref := range s.refs {
		if target, ok := autoRefID(source.FieldByName(ref.field)); ok {
			resolver.Add(tables[ref.key], target)
		}
	}
}

func autoRefID(v reflect.Value) (id.ID, bool) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return id.ID{}, false
		}
		v = v.Elem()
	}
	target := v.Interface().(id.ID)
	return target, !id.IsNil(target)
}

// AutoCreate is the create request body of a declared entity. It decodes
// into the derived create type with the strict options of the enclosing
// request and validates its binding rules.
type AutoCreate[T entity.DefinitionProvider] struct{ value reflect.Value }

// AutoUpdate is the update request body of a declared entity.
type AutoUpdate[T entity.DefinitionProvider] struct{ value reflect.Value }

func (r *AutoCreate[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	return decodeAuto(dec, autoShapeFor[T]().create, &r.value)
}

func (r *AutoUpdate[T]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	return decodeAuto(dec, autoShapeFor[T]().update, &r.value)
}

func decodeAuto(dec *jsontext.Decoder, t reflect.Type, out *reflect.Value) error {
	value := reflect.New(t)
	if err := json.UnmarshalDecode(dec, value.Interface()); err != nil {
		return err
	}
	if err := binding.Validator.ValidateStruct(value.Interface()); err != nil {
		return err
	}
	*out = value.Elem()
	return nil
}
