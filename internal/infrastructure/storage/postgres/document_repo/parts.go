package document_repo

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

// PartDocument is the constraint on a document model that has table parts.
type PartDocument interface{ GetID() id.ID }

// PartStore loads and stores the table parts a document Definition declares,
// with no code per part. A part is the model's []Line field whose json name
// is the part's Field. Line maps its columns with db tags and must declare
// line_id (id.ID) and line_no (int); its table is Definition.PartTable and
// references the header through document_id.
//
// SaveParts replaces a document's rows: lines keep the line_id they carry
// (a new one is assigned when it is nil) and are numbered by their position.
type PartStore[T PartDocument] struct {
	parts []storedPart
}

type storedPart struct {
	json    string // the part's collection name on the wire
	field   []int  // index of the []Line field in the model
	elem    reflect.Type
	table   string
	columns []string // line columns in field order
	lineID  []int
	lineNo  []int
}

// maxPartParameters keeps a multi-row INSERT under PostgreSQL's limit of
// bind parameters per statement.
const maxPartParameters = 65535

// NewPartStore derives the stores of def's parts from model T (a pointer to
// the model struct). It panics on a part the model does not declare as
// described on PartStore: that is a declaration error found at start-up.
func NewPartStore[T PartDocument](def entity.Definition) *PartStore[T] {
	model := reflect.TypeFor[T]()
	if model.Kind() != reflect.Pointer || model.Elem().Kind() != reflect.Struct {
		panic(fmt.Sprintf("table parts of %s: model %v must be a pointer to a struct", def.Key, model))
	}
	store := &PartStore[T]{}
	for _, part := range def.Parts {
		p, err := derivePart(model.Elem(), def, part)
		if err != nil {
			panic(fmt.Sprintf("table part %s.%s: %v", def.Key, part.Key, err))
		}
		store.parts = append(store.parts, p)
	}
	return store
}

// PartField returns the model field of a declared part, for derivations that
// follow the same binding (the wire shape of declarative registrations).
func PartField(model reflect.Type, part entity.Part) (reflect.StructField, bool) {
	for _, f := range reflect.VisibleFields(model) {
		if f.Anonymous || !f.IsExported() {
			continue
		}
		if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name == part.Field {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

func derivePart(model reflect.Type, def entity.Definition, part entity.Part) (storedPart, error) {
	f, ok := PartField(model, part)
	if !ok {
		return storedPart{}, fmt.Errorf("the model has no field with json name %q", part.Field)
	}
	if f.Type.Kind() != reflect.Slice || f.Type.Elem().Kind() != reflect.Struct {
		return storedPart{}, fmt.Errorf("field %s must be a slice of a line struct", f.Name)
	}
	if column, _, _ := strings.Cut(f.Tag.Get("db"), ","); column != "-" {
		return storedPart{}, fmt.Errorf("field %s must be tagged db:\"-\": its rows live in their own table", f.Name)
	}
	p := storedPart{json: part.Field, field: f.Index, elem: f.Type.Elem(), table: def.MustPartTable(part.Key)}
	for _, lf := range reflect.VisibleFields(p.elem) {
		if lf.Anonymous || !lf.IsExported() {
			continue
		}
		column, _, _ := strings.Cut(lf.Tag.Get("db"), ",")
		if column == "" || column == "-" {
			continue
		}
		switch column {
		case "document_id":
			return storedPart{}, fmt.Errorf("line %v must not map document_id: the store sets it", p.elem)
		case "line_id":
			if lf.Type != reflect.TypeFor[id.ID]() {
				return storedPart{}, fmt.Errorf("line_id of %v must be id.ID", p.elem)
			}
			p.lineID = lf.Index
		case "line_no":
			if lf.Type != reflect.TypeFor[int]() {
				return storedPart{}, fmt.Errorf("line_no of %v must be int", p.elem)
			}
			p.lineNo = lf.Index
		}
		p.columns = append(p.columns, column)
	}
	if p.lineID == nil || p.lineNo == nil {
		return storedPart{}, fmt.Errorf("line %v must map line_id and line_no", p.elem)
	}
	return p, nil
}

// RegisterFilters lets list filters of repo reach the parts' lines by the
// part's collection name and a line column, e.g. "items.product_id" (an
// EXISTS subquery on the part table).
func (s *PartStore[T]) RegisterFilters(repo *BaseDocumentRepo[T]) {
	for _, p := range s.parts {
		repo.RegisterTablePart(p.json, p.table, "document_id", postgres.ExtractFilterFieldsOf(p.elem))
	}
}

// LoadParts reads every table part of doc, ordered by line number. A part
// with no rows is an empty, non-nil slice.
func (s *PartStore[T]) LoadParts(ctx context.Context, doc T) error {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	target := reflect.ValueOf(doc).Elem()
	for _, p := range s.parts {
		query, args, err := squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar).
			Select(p.columns...).From(p.table).
			Where(squirrel.Eq{"document_id": doc.GetID()}).
			OrderBy("line_no").ToSql()
		if err != nil {
			return fmt.Errorf("build %s query: %w", p.table, err)
		}
		lines := reflect.New(reflect.SliceOf(p.elem))
		lines.Elem().Set(reflect.MakeSlice(reflect.SliceOf(p.elem), 0, 0))
		if err := pgxscan.Select(ctx, querier, lines.Interface(), query, args...); err != nil {
			return fmt.Errorf("load %s: %w", p.table, err)
		}
		target.FieldByIndex(p.field).Set(lines.Elem())
	}
	return nil
}

// SaveParts replaces the stored rows of every table part with doc's lines,
// assigning missing line ids and numbering lines from 1 on the model too.
func (s *PartStore[T]) SaveParts(ctx context.Context, doc T) error {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	docID := doc.GetID()
	source := reflect.ValueOf(doc).Elem()
	for _, p := range s.parts {
		if _, err := querier.Exec(ctx, "DELETE FROM "+p.table+" WHERE document_id = $1", docID); err != nil {
			return fmt.Errorf("clear %s: %w", p.table, err)
		}
		lines := source.FieldByIndex(p.field)
		columns := append([]string{"document_id"}, p.columns...)
		perChunk := maxPartParameters / len(columns)
		for start := 0; start < lines.Len(); start += perChunk {
			insert := squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar).Insert(p.table).Columns(columns...)
			for i := start; i < min(start+perChunk, lines.Len()); i++ {
				line := lines.Index(i)
				if lineID := line.FieldByIndex(p.lineID); id.IsNil(lineID.Interface().(id.ID)) {
					lineID.Set(reflect.ValueOf(id.New()))
				}
				line.FieldByIndex(p.lineNo).SetInt(int64(i + 1))
				values := postgres.StructToMap(line.Interface())
				row := make([]any, 0, len(columns))
				row = append(row, docID)
				for _, column := range p.columns {
					row = append(row, values[column])
				}
				insert = insert.Values(row...)
			}
			query, args, err := insert.ToSql()
			if err != nil {
				return fmt.Errorf("build %s insert: %w", p.table, err)
			}
			if _, err := querier.Exec(ctx, query, args...); err != nil {
				if postgres.IsForeignKeyViolation(err) {
					return apperror.NewBusinessRule("INVALID_REFERENCE", "Связанный элемент удален. Выберите другой.").
						WithDetail("field", p.json+"."+postgres.ExtractForeignKeyField(err, p.table))
				}
				return fmt.Errorf("store %s: %w", p.table, err)
			}
		}
	}
	return nil
}
