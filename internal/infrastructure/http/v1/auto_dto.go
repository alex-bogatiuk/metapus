package v1

import (
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin/binding"

	"metapus/internal/core/entity"
	"metapus/internal/core/json"
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
//     postedVersion), the writable fields and the model's read-only fields.
//
// Server-owned base fields (id, version, posting state, timestamps) are never
// writable, and a model field without a db column is never on the wire. Only
// the json and binding tags are carried over, so validation rules such as
// binding:"required" apply to the request as declared on the model.
type autoShape struct {
	create, update, read reflect.Type
	writable             []string // Go field names copied from a request onto the model
	readable             []string // Go field names copied from the model onto the response
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
		tag := `json:"` + jsonName + `"`
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

// response projects the model onto the response type.
func (s *autoShape) response(model any) any {
	source := reflect.ValueOf(model).Elem()
	out := reflect.New(s.read).Elem()
	out.FieldByName("ID").SetString(fmt.Sprint(source.FieldByName("ID").Interface()))
	for _, name := range s.readable {
		out.FieldByName(name).Set(source.FieldByName(name))
	}
	return out.Interface()
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
