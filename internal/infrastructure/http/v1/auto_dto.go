package v1

import (
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin/binding"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/document_repo"
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
//     without "Id", e.g. counterpartyId → counterparty: {id, name, code};
//   - table parts (documents): each part of the Definition is the model's
//     []Line field named by the part's Field (see document_repo.PartStore).
//     A request line carries the line's writable fields and, optionally, the
//     lineId of a line it keeps; a response line adds lineId, lineNo, the
//     read-only fields and reference displays, by the same rules as the
//     header. An update that omits a part keeps its stored lines; an empty
//     array clears them.
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
	parts                []autoPart
	calcs                autoCalcs // calculated header fields (meta:"calc:..."), in order
}

// autoPart is the wire projection of one table part.
type autoPart struct {
	field        string       // model []Line field
	elem         reflect.Type // model line type
	create, read reflect.Type // request and response line types
	contract     string       // PascalCase part name used in contract names
	lineID       string       // line field mapped to line_id
	lineNo       string       // line field mapped to line_no
	writable     []string
	readable     []string
	refs         []autoRef
	calcs        autoCalcs // calculated line fields, in order
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
	fields := newAutoFields()
	for _, b := range base {
		f, ok := model.FieldByName(b.name)
		if !ok {
			return nil, fmt.Errorf("standard field %s is missing", b.name)
		}
		if err := fields.add(&fields.writable, f, b.json, b.binding); err != nil {
			return nil, err
		}
	}
	if err := fields.addColumns(model, false, nil); err != nil {
		return nil, err
	}
	writable, readOnly := fields.writable, fields.readOnly
	for _, f := range writable {
		shape.writable = append(shape.writable, f.Name)
	}
	if len(def.Parts) > 0 && def.Kind != entity.DocumentKind {
		return nil, fmt.Errorf("declarative registration stores table parts of documents only")
	}
	var partRequest, partResponse []reflect.StructField
	for _, part := range def.Parts {
		p, err := deriveAutoPart(model, part)
		if err != nil {
			return nil, fmt.Errorf("table part %s: %w", part.Key, err)
		}
		if other, taken := fields.names[part.Field]; taken {
			return nil, fmt.Errorf("json name %q of table part %s is already used by %s", part.Field, part.Key, other)
		}
		fields.names[part.Field] = p.field
		shape.parts = append(shape.parts, p)
		partRequest = append(partRequest, reflect.StructField{Name: p.field, Type: reflect.SliceOf(p.create),
			Tag: reflect.StructTag(`json:"` + part.Field + `,omitempty" binding:"omitempty,dive"`)})
		partResponse = append(partResponse, reflect.StructField{Name: p.field, Type: reflect.SliceOf(p.read),
			Tag: reflect.StructTag(`json:"` + part.Field + `,omitzero"`)})
	}

	create := append(append([]reflect.StructField(nil), writable...), partRequest...)
	if def.Kind == entity.DocumentKind {
		create = append(create, reflect.StructField{Name: "PostImmediately", Type: reflect.TypeFor[bool](), Tag: `json:"postImmediately"`})
	}
	update := append(append(append([]reflect.StructField(nil), writable...), partRequest...),
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
	displays, refs, err := fields.refDisplays(model)
	if err != nil {
		return nil, err
	}
	read = append(append(read, displays...), partResponse...)
	shape.refs = refs

	calcs, err := deriveAutoCalcs(model, def.Kind)
	if err != nil {
		return nil, err
	}
	shape.calcs = calcs

	shape.create, shape.update, shape.read = reflect.StructOf(create), reflect.StructOf(update), reflect.StructOf(read)
	return shape, nil
}

// autoFields collects the wire fields of a model or a line struct, keeping
// json names unique.
type autoFields struct {
	writable, readOnly []reflect.StructField
	names              map[string]string // json name → Go field name
}

func newAutoFields() *autoFields { return &autoFields{names: map[string]string{}} }

func (a *autoFields) add(list *[]reflect.StructField, f reflect.StructField, jsonName, bindingTag string) error {
	if other, taken := a.names[jsonName]; taken {
		return fmt.Errorf("json name %q of %s is already used by %s", jsonName, f.Name, other)
	}
	a.names[jsonName] = f.Name
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

// addColumns adds the exported fields of t that have a db column and a json
// name, read-only ones apart; skip names columns the caller maps itself.
// Embedded structs are taken as declared (flattened) only for lines.
func (a *autoFields) addColumns(t reflect.Type, flatten bool, skip map[string]bool) error {
	var candidates []reflect.StructField
	if flatten {
		candidates = reflect.VisibleFields(t)
	} else {
		for i := 0; i < t.NumField(); i++ {
			candidates = append(candidates, t.Field(i))
		}
	}
	for _, f := range candidates {
		if f.Anonymous || !f.IsExported() {
			continue
		}
		jsonName, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		column, _, _ := strings.Cut(f.Tag.Get("db"), ",")
		if jsonName == "" || jsonName == "-" || column == "" || column == "-" || skip[column] {
			continue
		}
		list := &a.writable
		if autoReadOnly(f) {
			list = &a.readOnly
		}
		if err := a.add(list, f, jsonName, f.Tag.Get("binding")); err != nil {
			return err
		}
	}
	return nil
}

// refDisplays derives the response display of every reference field among
// the collected fields of model.
func (a *autoFields) refDisplays(model reflect.Type) ([]reflect.StructField, []autoRef, error) {
	var displays []reflect.StructField
	var refs []autoRef
	for _, f := range append(append([]reflect.StructField(nil), a.writable...), a.readOnly...) {
		original, _ := model.FieldByName(f.Name)
		key := autoRefKey(original)
		jsonName, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if key == "" || (f.Type != reflect.TypeFor[id.ID]() && f.Type != reflect.TypeFor[*id.ID]()) || !strings.HasSuffix(jsonName, "Id") {
			continue
		}
		displayJSON := strings.TrimSuffix(jsonName, "Id")
		if other, taken := a.names[displayJSON]; taken {
			return nil, nil, fmt.Errorf("reference display %q of %s is already used by %s", displayJSON, f.Name, other)
		}
		a.names[displayJSON] = f.Name
		display := strings.TrimSuffix(f.Name, "ID") + "Ref"
		displays = append(displays, reflect.StructField{Name: display, Type: reflect.TypeFor[*postgres.RefDisplay](), Tag: reflect.StructTag(`json:"` + displayJSON + `,omitempty"`)})
		refs = append(refs, autoRef{field: f.Name, display: display, key: key})
	}
	return displays, refs, nil
}

func deriveAutoPart(model reflect.Type, part entity.Part) (autoPart, error) {
	f, ok := document_repo.PartField(model, part)
	if !ok {
		return autoPart{}, fmt.Errorf("the model has no field with json name %q", part.Field)
	}
	if f.Type.Kind() != reflect.Slice || f.Type.Elem().Kind() != reflect.Struct || len(f.Index) != 1 {
		return autoPart{}, fmt.Errorf("field %s must be a slice of a line struct declared on the model", f.Name)
	}
	p := autoPart{field: f.Name, elem: f.Type.Elem(), contract: strings.ToUpper(part.Field[:1]) + part.Field[1:]}
	for _, lf := range reflect.VisibleFields(p.elem) {
		switch column, _, _ := strings.Cut(lf.Tag.Get("db"), ","); column {
		case "line_id":
			p.lineID = lf.Name
		case "line_no":
			p.lineNo = lf.Name
		}
	}
	if p.lineID == "" || p.lineNo == "" {
		return autoPart{}, fmt.Errorf("line %v must map line_id and line_no", p.elem)
	}
	fields := newAutoFields()
	fields.names["lineId"], fields.names["lineNo"] = p.lineID, p.lineNo
	if err := fields.addColumns(p.elem, true, map[string]bool{"line_id": true, "line_no": true, "document_id": true}); err != nil {
		return autoPart{}, err
	}
	displays, refs, err := fields.refDisplays(p.elem)
	if err != nil {
		return autoPart{}, err
	}
	for _, wf := range fields.writable {
		p.writable = append(p.writable, wf.Name)
	}
	if p.calcs, err = deriveAutoCalcs(p.elem, entity.DocumentKind); err != nil {
		return autoPart{}, err
	}
	create := append([]reflect.StructField{{Name: "LineID", Type: reflect.TypeFor[*id.ID](), Tag: `json:"lineId,omitempty"`}}, fields.writable...)
	read := []reflect.StructField{
		{Name: "LineID", Type: reflect.TypeFor[id.ID](), Tag: `json:"lineId"`},
		{Name: "LineNo", Type: reflect.TypeFor[int](), Tag: `json:"lineNo"`},
	}
	for _, rf := range append(append([]reflect.StructField(nil), fields.writable...), fields.readOnly...) {
		p.readable = append(p.readable, rf.Name)
	}
	read = append(append(append(read, fields.writable...), fields.readOnly...), displays...)
	p.create, p.read, p.refs = reflect.StructOf(create), reflect.StructOf(read), refs
	return p, nil
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
	// A calculated field is the server's: requests never carry it.
	if calcTag(f) != "" {
		return true
	}
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
// model. An omitted attributes object or table part keeps the stored one.
func (s *autoShape) applyAutoRequest(request reflect.Value, model any) {
	target := reflect.ValueOf(model).Elem()
	for _, name := range s.writable {
		value := request.FieldByName(name)
		if name == "Attributes" && value.IsNil() {
			continue
		}
		target.FieldByName(name).Set(value)
	}
	for _, part := range s.parts {
		part.apply(request.FieldByName(part.field), target.FieldByName(part.field))
	}
}

// computeAutoCalcs computes the calculated fields of every line and then of
// the header, after the request is applied (a kept line's inputs may have
// changed); lines go first so a header total sees their calculated values.
func (s *autoShape) computeAutoCalcs(model any) error {
	target := reflect.ValueOf(model).Elem()
	for _, part := range s.parts {
		if len(part.calcs) == 0 {
			continue
		}
		lines := target.FieldByName(part.field)
		for i := 0; i < lines.Len(); i++ {
			if err := part.calcs.apply(lines.Index(i)); err != nil {
				return err
			}
		}
	}
	return s.calcs.apply(target)
}

// apply replaces the model's lines with the requested ones. A requested
// lineId is kept only when it names a stored line of this document not yet
// taken by an earlier line; that line keeps its read-only fields (filled by
// the server, e.g. at posting) and takes the request's writable ones. Every
// other line is new, with a new id. Lines are numbered by position.
func (p autoPart) apply(requested, stored reflect.Value) {
	if requested.IsNil() {
		if stored.IsNil() {
			stored.Set(reflect.MakeSlice(stored.Type(), 0, 0))
		}
		return
	}
	known := map[id.ID]reflect.Value{}
	for i := 0; i < stored.Len(); i++ {
		line := stored.Index(i)
		known[line.FieldByName(p.lineID).Interface().(id.ID)] = line
	}
	lines := reflect.MakeSlice(stored.Type(), requested.Len(), requested.Len())
	for i := 0; i < requested.Len(); i++ {
		in, line := requested.Index(i), lines.Index(i)
		lineID := id.New()
		if keep := in.FieldByName("LineID"); !keep.IsNil() {
			candidate := keep.Elem().Interface().(id.ID)
			if previous, ok := known[candidate]; ok {
				line.Set(previous)
				lineID = candidate
				delete(known, candidate)
			}
		}
		for _, name := range p.writable {
			line.FieldByName(name).Set(in.FieldByName(name))
		}
		line.FieldByName(p.lineID).Set(reflect.ValueOf(lineID))
		line.FieldByName(p.lineNo).SetInt(int64(i + 1))
	}
	stored.Set(lines)
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
	setRefDisplays(s.refs, source, out, tables, refs)
	for _, part := range s.parts {
		lines := source.FieldByName(part.field)
		if lines.IsNil() {
			continue // not loaded, e.g. a list row
		}
		projected := reflect.MakeSlice(reflect.SliceOf(part.read), lines.Len(), lines.Len())
		for i := 0; i < lines.Len(); i++ {
			line, into := lines.Index(i), projected.Index(i)
			into.FieldByName("LineID").Set(line.FieldByName(part.lineID))
			into.FieldByName("LineNo").Set(line.FieldByName(part.lineNo))
			for _, name := range part.readable {
				into.FieldByName(name).Set(line.FieldByName(name))
			}
			setRefDisplays(part.refs, line, into, tables, refs)
		}
		out.FieldByName(part.field).Set(projected)
	}
	return out.Interface()
}

func setRefDisplays(list []autoRef, source, out reflect.Value, tables map[string]string, refs postgres.ResolvedRefs) {
	if refs == nil {
		return
	}
	for _, ref := range list {
		if target, ok := autoRefID(source.FieldByName(ref.field)); ok {
			if display := refs.Get(tables[ref.key], target); display.ID != "" {
				out.FieldByName(ref.display).Set(reflect.ValueOf(&display))
			}
		}
	}
}

// allRefs lists the reference fields of the header and of every table part.
func (s *autoShape) allRefs() []autoRef {
	all := append([]autoRef(nil), s.refs...)
	for _, part := range s.parts {
		all = append(all, part.refs...)
	}
	return all
}

// collectRefs registers the model's references, its lines' included, with
// the resolver.
func (s *autoShape) collectRefs(resolver *postgres.ReferenceResolver, tables map[string]string, model any) {
	source := reflect.ValueOf(model).Elem()
	add := func(list []autoRef, v reflect.Value) {
		for _, ref := range list {
			if target, ok := autoRefID(v.FieldByName(ref.field)); ok {
				resolver.Add(tables[ref.key], target)
			}
		}
	}
	add(s.refs, source)
	for _, part := range s.parts {
		lines := source.FieldByName(part.field)
		for i := 0; i < lines.Len(); i++ {
			add(part.refs, lines.Index(i))
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
