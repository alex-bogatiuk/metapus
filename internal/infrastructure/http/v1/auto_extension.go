package v1

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/contracts/spec"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

// Extensions: an add-on declares fields of its own for a catalog of its base
// or of the platform, and the platform serves them with the catalog.
//
//	type ItemAutomotive struct {
//		Brand             string `db:"brand" json:"brand" meta:"label:Бренд"`
//		NormalizedArticle string `db:"normalized_article" json:"normalizedArticle" meta:"label:Нормализованный артикул,readOnly:true"`
//	}
//
//	reg.RegisterExtension(v1.Extend[item.Item, ItemAutomotive]("automotive", "Автобизнес").
//		OnWrite(func(ctx context.Context, it *item.Item, ext *ItemAutomotive) error { ... }).
//		Search("normalized_article", NormalizeArticle))
//
// The fields are stored in the add-on's table <catalog table>__<add-on>
// (cat_items__automotive), one row per item, whose id is the item's id; the
// add-on's migration creates it, and the catalog's table is never altered.
// The platform reads and writes the row with the item in the item's
// transaction, and the fields are on the wire flat, beside the catalog's
// own: the item's create, update and read bodies, its list and its list
// filters, its form (in a group named after the add-on), its file import and
// its field policies all take them by their json and column names, so a
// form, a filter or a file column treats them as the catalog's fields. An
// update that omits an extension field keeps its stored value. A json name
// or a column already used by the catalog fails the registration, so a base
// that later adds a field of the same name breaks its add-on at its first
// start, not silently.

// extensionOwner is an add-on name: it names the extension table.
var extensionOwner = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// extensionSpec is the description of an extension that the registry, the
// catalog's shape and its metadata share.
type extensionSpec struct {
	owner  string // the add-on that declares it
	group  string // the form group of its fields
	target entity.Definition
	model  reflect.Type // the extension's struct
	fields []extensionField
	search *catalog_repo.ExtensionSearch
}

// extensionField is one field of an extension.
type extensionField struct {
	name, json, column string
	field              reflect.StructField
	readOnly           bool
}

// table is the extension's table: <catalog table>__<add-on>.
func (s *extensionSpec) table() string { return s.target.Table + "__" + s.owner }

// contractName prefixes the extension's contracts: ItemAutomotive.
func (s *extensionSpec) contractName() string {
	var b strings.Builder
	b.WriteString(s.target.Name)
	for part := range strings.SplitSeq(s.owner, "_") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

func deriveExtensionSpec(owner, group string, target entity.Definition, model reflect.Type) (*extensionSpec, error) {
	if !extensionOwner.MatchString(owner) {
		return nil, fmt.Errorf("add-on name %q must be a lowercase identifier", owner)
	}
	if target.Kind != entity.CatalogKind {
		return nil, fmt.Errorf("%s is not a catalog: extensions of documents come later", target.Key)
	}
	if model.Kind() != reflect.Struct {
		return nil, fmt.Errorf("extension %v must be a struct", model)
	}
	s := &extensionSpec{owner: owner, group: group, target: target, model: model}
	names := map[string]string{}
	for i := 0; i < model.NumField(); i++ {
		f := model.Field(i)
		if f.Anonymous {
			return nil, fmt.Errorf("extension %v: embedded field %s; declare the fields themselves", model, f.Name)
		}
		if !f.IsExported() {
			continue
		}
		jsonName, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		column, _, _ := strings.Cut(f.Tag.Get("db"), ",")
		if jsonName == "" || jsonName == "-" || column == "" || column == "-" {
			return nil, fmt.Errorf("extension %v: field %s needs a db column and a json name", model, f.Name)
		}
		if column == "id" {
			return nil, fmt.Errorf("extension %v: id is the item's id, not a field", model)
		}
		if snakeCase(jsonName) != column {
			return nil, fmt.Errorf("extension %v: field %s: column %q must be the json name %q in snake case", model, f.Name, column, jsonName)
		}
		if autoRefKey(f) != "" {
			return nil, fmt.Errorf("extension %v: reference field %s: references in extensions come later", model, f.Name)
		}
		if calcTag(f) != "" {
			return nil, fmt.Errorf("extension %v: calculated field %s: an extension computes its fields in its OnWrite hook", model, f.Name)
		}
		if other, taken := names[jsonName]; taken {
			return nil, fmt.Errorf("extension %v: json name %q of %s is already used by %s", model, jsonName, f.Name, other)
		}
		names[jsonName] = f.Name
		s.fields = append(s.fields, extensionField{name: f.Name, json: jsonName, column: column, field: f, readOnly: autoReadOnly(f)})
	}
	if len(s.fields) == 0 {
		return nil, fmt.Errorf("extension %v declares no fields", model)
	}
	return s, nil
}

// snakeCase is the column of a json name, as the frontend's filters and
// field policies derive it (camelToSnake).
func snakeCase(name string) string {
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// checkTarget refuses an extension whose json names or columns the
// catalog's shape already uses.
func (s *extensionSpec) checkTarget(shape *autoShape) error {
	jsonNames, columns := map[string]bool{}, map[string]bool{"id": true}
	for _, t := range []reflect.Type{shape.read, shape.create, shape.update} {
		for i := 0; i < t.NumField(); i++ {
			name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
			jsonNames[name] = true
		}
	}
	for _, f := range reflect.VisibleFields(shape.model) {
		column, _, _ := strings.Cut(f.Tag.Get("db"), ",")
		columns[column] = true
	}
	for _, f := range s.fields {
		if jsonNames[f.json] {
			return fmt.Errorf("extension %s of %s: json name %q is already a field of the catalog", s.owner, s.target.Key, f.json)
		}
		if columns[f.column] {
			return fmt.Errorf("extension %s of %s: column %q is already a column of the catalog", s.owner, s.target.Key, f.column)
		}
	}
	return nil
}

// valueOf is the extension's value on model, a new zero one when it has
// none and create is set; invalid when it has none.
func (s *extensionSpec) valueOf(model any, create bool) reflect.Value {
	extensible, ok := model.(entity.Extensible)
	if !ok {
		return reflect.Value{}
	}
	values := extensible.ExtensionValues()
	if found := values.Find(reflect.PointerTo(s.model)); found != nil {
		return reflect.ValueOf(found).Elem()
	}
	if !create {
		return reflect.Value{}
	}
	value := reflect.New(s.model)
	values.Put(value.Interface())
	return value.Elem()
}

// metadataFields are the extension's fields in the catalog's metadata, in
// the add-on's form group. They have no column of the catalog's table, so
// reports and authorized reads, which select the catalog's columns, leave
// them out; the catalog's routes serve them.
func (s *extensionSpec) metadataFields() []metadata.FieldDef {
	inspected := metadata.Inspect(reflect.New(s.model).Elem().Interface(), s.model.Name(), metadata.TypeCatalog)
	fields := make([]metadata.FieldDef, 0, len(inspected.Fields))
	for _, f := range inspected.Fields {
		f.DBColumn = ""
		if f.Group == "" {
			f.Group = s.group
		}
		fields = append(fields, f)
	}
	return fields
}

// requestType is the extension's part of a create or update body: every
// writable field optional, so an update that omits one keeps it.
func (s *extensionSpec) requestType(wire func(extensionField) string) reflect.Type {
	var fields []reflect.StructField
	for _, f := range s.fields {
		if f.readOnly {
			continue
		}
		tag := `json:"` + f.json + `,omitempty"`
		if b := f.field.Tag.Get("binding"); b != "" {
			tag += ` binding:"` + b + `"`
		}
		fields = append(fields, reflect.StructField{Name: wire(f), Type: optional(f.field.Type), Tag: reflect.StructTag(tag)})
	}
	return reflect.StructOf(fields)
}

// responseType is the extension's part of a response: every field.
func (s *extensionSpec) responseType(wire func(extensionField) string) reflect.Type {
	fields := make([]reflect.StructField, 0, len(s.fields))
	for _, f := range s.fields {
		tag := `json:"` + f.json
		if _, options, ok := strings.Cut(f.field.Tag.Get("json"), ","); ok {
			tag += "," + options
		}
		fields = append(fields, reflect.StructField{Name: wire(f), Type: f.field.Type, Tag: reflect.StructTag(tag + `"`)})
	}
	return reflect.StructOf(fields)
}

// optional is the request type of a field: a pointer, nil when omitted.
func optional(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		return t
	}
	return reflect.PointerTo(t)
}

func ownName(f extensionField) string { return f.name }

// HTTPContracts of an extension are its parts of the catalog's create,
// update and read bodies, <Catalog><AddOn>Request and Response, and the
// catalog's routes they extend: the add-on's frontend route table merges
// them into the base's (registerHTTPRoutes).
func (s *extensionSpec) HTTPContracts() spec.Bundle {
	name := s.contractName()
	request, response := name+"Request", name+"Response"
	bundle := spec.Bundle{Contracts: []spec.Contract{
		{Name: request, Type: s.requestType(ownName), Direction: spec.Input},
		{Name: response, Type: s.responseType(ownName), Direction: spec.Output},
	}}
	base := "/" + string(s.target.Kind) + "/" + s.target.Route
	bundle.Routes = append(bundle.Routes,
		spec.RouteContract{Scope: "api", Method: "GET", Path: base + "/:id", Response: response, Extends: true},
		spec.RouteContract{Scope: "api", Method: "POST", Path: base, Request: request, Response: response, Extends: true},
		spec.RouteContract{Scope: "api", Method: "PUT", Path: base + "/:id", Request: request, Response: response, Extends: true},
	)
	return bundle
}

// ExtensionRegistration is an extension declared with Extend; add it with
// FactoryRegistry.RegisterExtension.
type ExtensionRegistration interface {
	spec.Provider
	extension() *extensionSpec
}

// catalogExtension installs an extension on the service of its catalog.
type catalogExtension[PT entity.CatalogEntity] interface {
	install(service *domain.CatalogService[PT], spec *extensionSpec)
}

// Extension declares fields of the add-on owner for the catalog of model T:
// the exported fields of E with a db column and a json name (meta labels,
// readOnly and binding rules as on a model). See the package comment above.
type Extension[T entity.DefinitionProvider, E any, PT catalogModel[T]] struct {
	spec    *extensionSpec
	onWrite []func(context.Context, PT, *E) error
}

// Extend declares the add-on owner's extension E of the catalog of model T;
// its fields are shown in the form group named group.
func Extend[T entity.DefinitionProvider, E any, PT catalogModel[T]](owner, group string) *Extension[T, E, PT] {
	var zero T
	s, err := deriveExtensionSpec(owner, group, zero.EntityDefinition(), reflect.TypeFor[E]())
	if err != nil {
		panic(fmt.Sprintf("extension of %v: %v", reflect.TypeFor[T](), err))
	}
	return &Extension[T, E, PT]{spec: s}
}

// OnWrite runs fn before every write of an item, in its transaction, after
// the item and its extension are validated: fn computes the extension's
// server-owned fields (meta:"readOnly:true") from the item.
func (x *Extension[T, E, PT]) OnWrite(fn func(ctx context.Context, item PT, ext *E) error) *Extension[T, E, PT] {
	x.onWrite = append(x.onWrite, fn)
	return x
}

// Search makes the catalog's list search also find an item whose column
// starts with the query, normalized by normalize as the column's values are
// (nil keeps the query as typed). The column should be indexed.
func (x *Extension[T, E, PT]) Search(column string, normalize func(string) string) *Extension[T, E, PT] {
	found := false
	for _, f := range x.spec.fields {
		found = found || f.column == column
	}
	if !found {
		panic(fmt.Sprintf("extension %v: search column %q is not one of its columns", x.spec.model, column))
	}
	x.spec.search = &catalog_repo.ExtensionSearch{Column: column, Normalize: normalize}
	return x
}

// Of is the extension's value on item, a zero one added when it has none.
func (x *Extension[T, E, PT]) Of(item PT) *E {
	return x.spec.valueOf(any(item), true).Addr().Interface().(*E)
}

// Table is the extension's table, <catalog table>__<add-on>.
func (x *Extension[T, E, PT]) Table() string { return x.spec.table() }

func (x *Extension[T, E, PT]) extension() *extensionSpec { return x.spec }

func (x *Extension[T, E, PT]) HTTPContracts() spec.Bundle { return x.spec.HTTPContracts() }

// install stores the extension with the catalog's items and runs its checks
// and hooks before every write: the field policies of its fields, its
// Validate and its OnWrite hooks.
func (x *Extension[T, E, PT]) install(service *domain.CatalogService[PT], s *extensionSpec) {
	table := catalog_repo.NewExtensionTable(s.target.Key, s.table(), s.model, s.search)
	var repo *catalog_repo.ExtendedRepository[PT]
	service.WrapRepository(func(inner domain.CatalogRepository[PT]) domain.CatalogRepository[PT] {
		repo = catalog_repo.NewExtendedRepository(inner, table)
		return repo
	})
	entityName := service.EntityName()
	service.Hooks().OnBeforeCreate(func(ctx context.Context, item PT) error {
		return x.beforeWrite(ctx, entityName, item, reflect.Value{})
	})
	service.Hooks().OnBeforeUpdate(func(ctx context.Context, item PT) error {
		stored, err := repo.LoadStored(ctx, item.GetID())
		if err != nil {
			return err
		}
		return x.beforeWrite(ctx, entityName, item, reflect.ValueOf(stored).Elem())
	})
}

func (x *Extension[T, E, PT]) beforeWrite(ctx context.Context, entityName string, item PT, stored reflect.Value) error {
	value := x.spec.valueOf(any(item), true)
	if err := checkExtensionWrite(ctx, entityName, x.spec, value, stored); err != nil {
		return err
	}
	ext := value.Addr().Interface().(*E)
	if validatable, ok := any(ext).(entity.Validatable); ok {
		if err := validatable.Validate(ctx); err != nil {
			if apperror.IsAppError(err) {
				return err
			}
			return apperror.NewValidation("extension validation failed").WithCause(err)
		}
	}
	for _, fn := range x.onWrite {
		if err := fn(ctx, item, ext); err != nil {
			return err
		}
	}
	return nil
}

// checkExtensionWrite applies the write field policy to the extension's
// writable fields, as the platform does to the catalog's own: a new item may
// not fill a field the editor may not write; an edit may not change one, and
// a field the editor can neither read nor write, which the masked form sent
// back empty, keeps its stored value. stored is invalid for a new item.
func checkExtensionWrite(ctx context.Context, entityName string, s *extensionSpec, value, stored reflect.Value) error {
	write := security.GetFieldPolicy(ctx, entityName, "write")
	if write == nil {
		return nil
	}
	read := security.GetFieldPolicy(ctx, entityName, "read")
	for _, f := range s.fields {
		if f.readOnly || write.IsFieldAllowed(f.column) {
			continue
		}
		current := value.FieldByName(f.name)
		if !stored.IsValid() {
			if !current.IsZero() {
				return apperror.NewForbidden(fmt.Sprintf("field '%s' is read-only", f.column)).WithDetail("field", f.column)
			}
			continue
		}
		previous := stored.FieldByName(f.name)
		if read != nil && !read.IsFieldAllowed(f.column) && current.IsZero() {
			current.Set(previous)
			continue
		}
		if !reflect.DeepEqual(current.Interface(), previous.Interface()) {
			return apperror.NewForbidden(fmt.Sprintf("field '%s' is read-only", f.column)).WithDetail("field", f.column)
		}
	}
	return nil
}

// maskExtensions zeroes, in read projections, the extension fields the
// reader's field policy hides.
func maskExtensions[PT any](ctx context.Context, entityName string, specs []*extensionSpec, items []PT) {
	policy := security.GetFieldPolicy(ctx, entityName, "read")
	if policy == nil {
		return
	}
	for _, s := range specs {
		for _, item := range items {
			value := s.valueOf(any(item), false)
			if !value.IsValid() {
				continue
			}
			for _, f := range s.fields {
				if !policy.IsFieldAllowed(f.column) {
					value.FieldByName(f.name).SetZero()
				}
			}
		}
	}
}

// RegisterExtension adds an add-on's extension of a catalog. The catalog may
// be registered before or after it (the platform's catalogs come first, the
// base's next, the add-on's last); the server refuses to start when it is
// missing or is not a declared catalog (Catalog).
func (r *FactoryRegistry) RegisterExtension(ext ExtensionRegistration) {
	s := ext.extension()
	for _, existing := range r.extensions {
		other := existing.extension()
		if other.target.Key == s.target.Key && (other.owner == s.owner || other.model == s.model) {
			panic(fmt.Sprintf("duplicate extension %s of %s", s.owner, s.target.Key))
		}
		if other.target.Key == s.target.Key {
			for _, f := range s.fields {
				for _, g := range other.fields {
					if f.json == g.json || f.column == g.column {
						panic(fmt.Sprintf("extension %s of %s: field %s is already declared by %s", s.owner, s.target.Key, f.json, other.owner))
					}
				}
			}
		}
	}
	r.extensions = append(r.extensions, ext)
}

// Extensions returns the registered extensions.
func (r *FactoryRegistry) Extensions() []ExtensionRegistration { return r.extensions }

// extensionsOf are the extensions of the entity key.
func (r *FactoryRegistry) extensionsOf(key string) []ExtensionRegistration {
	var result []ExtensionRegistration
	for _, ext := range r.extensions {
		if ext.extension().target.Key == key {
			result = append(result, ext)
		}
	}
	return result
}

// checkExtensionTargets refuses an extension whose catalog is not
// registered or not declared with Catalog.
func (r *FactoryRegistry) checkExtensionTargets() {
	for _, ext := range r.extensions {
		s := ext.extension()
		found := false
		for _, c := range r.catalogs {
			if c.EntityDefinition().Key != s.target.Key {
				continue
			}
			if _, ok := c.(extensibleCatalog); !ok {
				panic(fmt.Sprintf("extension %s of %s: the catalog is not declared with Catalog", s.owner, s.target.Key))
			}
			found = true
		}
		if !found {
			panic(fmt.Sprintf("extension %s of %s: the catalog is not registered", s.owner, s.target.Key))
		}
	}
}

// extensibleCatalog is a declared catalog: it serves the extensions
// registered for it.
type extensibleCatalog interface {
	extensionSpecs() []*extensionSpec
}

// extensionSpecs are the extensions of the catalog, checked against its shape.
func (r *AutoCatalog[T, PT]) extensionSpecs() []*extensionSpec {
	if r.registry == nil {
		return nil
	}
	base := autoShapeFor[T]()
	var specs []*extensionSpec
	for _, ext := range r.registry.extensionsOf(r.EntityDefinition().Key) {
		s := ext.extension()
		if err := s.checkTarget(base); err != nil {
			panic(err.Error())
		}
		specs = append(specs, s)
	}
	return specs
}

// appendExtensionMetadata adds the fields of the catalog's extensions to its
// metadata.
func appendExtensionMetadata(def *metadata.EntityDef, factory CatalogRegistration) {
	catalog, ok := factory.(extensibleCatalog)
	if !ok {
		return
	}
	for _, s := range catalog.extensionSpecs() {
		def.Fields = append(def.Fields, s.metadataFields()...)
	}
}

// autoExtension is an extension's part of a catalog's shape: its fields go
// into the merged request and response types under wire names of their own,
// ExtN_<field>, so they never clash with the catalog's Go field names.
type autoExtension struct {
	spec  *extensionSpec
	index int
}

func (e autoExtension) wire(f extensionField) string { return fmt.Sprintf("Ext%d_%s", e.index, f.name) }

// extendAutoShape is the shape of a catalog with its extensions: the
// catalog's request and response types with the extensions' fields added.
func extendAutoShape(base *autoShape, specs []*extensionSpec) *autoShape {
	if len(specs) == 0 {
		return base
	}
	shape := *base
	fieldsOf := func(t reflect.Type) []reflect.StructField {
		fields := make([]reflect.StructField, t.NumField())
		for i := range fields {
			fields[i] = t.Field(i)
		}
		return fields
	}
	create, update, read := fieldsOf(base.create), fieldsOf(base.update), fieldsOf(base.read)
	shape.extensions = nil
	for i, s := range specs {
		ext := autoExtension{spec: s, index: i}
		shape.extensions = append(shape.extensions, ext)
		request := fieldsOf(s.requestType(ext.wire))
		create = append(create, request...)
		update = append(update, request...)
		read = append(read, fieldsOf(s.responseType(ext.wire))...)
	}
	shape.create, shape.update, shape.read = reflect.StructOf(create), reflect.StructOf(update), reflect.StructOf(read)
	return &shape
}

// applyExtensions copies the extension fields of a request onto the model;
// an omitted field keeps the model's value.
func (s *autoShape) applyExtensions(request reflect.Value, model any) {
	for _, ext := range s.extensions {
		value := ext.spec.valueOf(model, true)
		for _, f := range ext.spec.fields {
			if f.readOnly {
				continue
			}
			in := request.FieldByName(ext.wire(f))
			if !in.IsValid() || in.IsNil() {
				continue
			}
			if in.Type() == f.field.Type {
				value.FieldByName(f.name).Set(in)
			} else {
				value.FieldByName(f.name).Set(in.Elem())
			}
		}
	}
}

// respondExtensions copies the model's extension fields onto a response; a
// model without an extension value shows its zero value.
func (s *autoShape) respondExtensions(model any, out reflect.Value) {
	for _, ext := range s.extensions {
		value := ext.spec.valueOf(model, false)
		if !value.IsValid() {
			continue
		}
		for _, f := range ext.spec.fields {
			out.FieldByName(ext.wire(f)).Set(value.FieldByName(f.name))
		}
	}
}

// labelExtensionImports names the import fields of the extensions by their
// labels, as the catalog's own fields are named.
func labelExtensionImports[T entity.DefinitionProvider, PT catalogModel[T]](im *importer[T, PT], shape *autoShape) {
	labels := map[string]string{}
	for _, ext := range shape.extensions {
		for _, f := range ext.spec.fields {
			if label := autoMetaValue(f.field, "label"); label != "" {
				labels[ext.wire(f)] = label
			}
		}
	}
	for i, f := range im.fields {
		if label, ok := labels[f.field.Name]; ok {
			im.fields[i].label = label
		}
	}
}

// installExtensions installs the catalog's extensions on its service.
func (r *AutoCatalog[T, PT]) installExtensions(service *domain.CatalogService[PT]) {
	if r.registry == nil {
		return
	}
	for _, ext := range r.registry.extensionsOf(r.EntityDefinition().Key) {
		installer, ok := ext.(catalogExtension[PT])
		if !ok {
			panic(fmt.Sprintf("extension %s of %s is not declared for its model %v", ext.extension().owner, r.EntityDefinition().Key, reflect.TypeFor[T]()))
		}
		installer.install(service, ext.extension())
	}
}

// maskedExtensionMappers wraps the reference mappers of a catalog with
// extensions: the read projections lose the extension fields the reader's
// field policy hides before references resolve and the response is built.
func maskedExtensionMappers[PT any](entityName string, specs []*extensionSpec, shape *autoShape, tables map[string]string,
	resolve func(context.Context, ...PT) (any, error), mapWithRefs func(PT, any) any) (func(context.Context, ...PT) (any, error), func(PT, any) any) {
	if len(specs) == 0 {
		return resolve, mapWithRefs
	}
	masked := func(ctx context.Context, items ...PT) (any, error) {
		maskExtensions(ctx, entityName, specs, items)
		if resolve == nil {
			return noRefs{}, nil
		}
		return resolve(ctx, items...)
	}
	if mapWithRefs == nil {
		mapWithRefs = func(model PT, _ any) any { return shape.response(model, tables, nil) }
	}
	return masked, mapWithRefs
}

// noRefs is the reference bag of a catalog without references: the masking
// of its extensions still runs before its responses are built.
type noRefs struct{}
