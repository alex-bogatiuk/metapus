package v1

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/core/tx"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/handlers"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/importing"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

// CatalogImportHandler is a catalog handler that loads items from a file
// (загрузка данных): POST /import/preview shows the file's columns and the
// fields they may fill, POST /import loads it.
type CatalogImportHandler interface {
	ImportPreview(c *gin.Context)
	Import(c *gin.Context)
}

// importCatalogHandler adds the import routes to a declared catalog's
// handler; the embedded handler keeps its optional interfaces.
type importCatalogHandler[PT entity.CatalogEntity, C, U any] struct {
	*handlers.CatalogHandler[PT, C, U]
	preview, run gin.HandlerFunc
}

func (h importCatalogHandler[PT, C, U]) ImportPreview(c *gin.Context) { h.preview(c) }
func (h importCatalogHandler[PT, C, U]) Import(c *gin.Context)        { h.run(c) }

// importField is a field of the create request a column may fill.
type importField struct {
	name, label string
	field       reflect.StructField
	required    bool
	refTable    string // the table a reference is looked up in, "" for none
	parent      bool   // the hierarchy's parent: looked up among folders
}

// importer loads rows of a file into a declared catalog through its service,
// as its create and update routes would.
type importer[T entity.DefinitionProvider, PT catalogModel[T]] struct {
	def     entity.Definition
	shape   *autoShape
	service *domain.CatalogService[PT]
	fields  []importField
}

func newImporter[T entity.DefinitionProvider, PT catalogModel[T]](def entity.Definition, shape *autoShape, service *domain.CatalogService[PT], tables map[string]string) *importer[T, PT] {
	model := reflect.TypeFor[T]()
	im := &importer[T, PT]{def: def, shape: shape, service: service}
	for i := range shape.create.NumField() {
		f := shape.create.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" || name == "attributes" {
			continue
		}
		field := importField{name: name, label: name, field: f, required: strings.Contains(f.Tag.Get("binding"), "required")}
		if original, ok := model.FieldByName(f.Name); ok {
			if label := autoMetaValue(original, "label"); label != "" {
				field.label = label
			}
			if key := autoRefKey(original); key != "" {
				field.refTable = tables[key]
			}
		}
		switch f.Name {
		case "Code":
			field.label = "Код"
		case "Name":
			field.label = "Наименование"
		case "ParentID":
			field.label, field.refTable, field.parent = "Группа", def.Table, true
		case "IsFolder":
			field.label = "Это группа"
		}
		im.fields = append(im.fields, field)
	}
	return im
}

func autoMetaValue(f reflect.StructField, key string) string {
	for part := range strings.SplitSeq(f.Tag.Get("meta"), ",") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(part), key+":"); ok {
			return value
		}
	}
	return ""
}

func (im *importer[T, PT]) fieldByName(name string) (importField, bool) {
	for _, f := range im.fields {
		if f.name == name {
			return f, true
		}
	}
	return importField{}, false
}

// readFile decodes and reads the uploaded file.
func readFile(fileName, content string) (*importing.Table, error) {
	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, apperror.NewValidation("Содержимое файла должно быть в base64").WithCause(err)
	}
	return importing.Read(fileName, data)
}

// preview answers the file's columns with the fields they look like, the
// catalog's fields and the first rows.
func (im *importer[T, PT]) preview(c *gin.Context) {
	var req dto.ImportPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		return
	}
	table, err := readFile(req.FileName, req.Content)
	if err != nil {
		_ = c.Error(err)
		return
	}
	resp := dto.ImportPreviewResponse{Total: len(table.Rows), Rows: [][]string{}}
	for _, f := range im.fields {
		resp.Fields = append(resp.Fields, dto.ImportField{Name: f.name, Label: f.label, Required: f.required, Reference: f.refTable != ""})
	}
	taken := map[string]bool{}
	for _, header := range table.Header {
		column := dto.ImportColumn{Header: header}
		wanted := strings.ToLower(strings.TrimSpace(header))
		for _, f := range im.fields {
			if !taken[f.name] && (strings.ToLower(f.label) == wanted || strings.ToLower(f.name) == wanted) {
				column.Field, taken[f.name] = f.name, true
				break
			}
		}
		resp.Columns = append(resp.Columns, column)
	}
	for i := 0; i < len(table.Rows) && i < 10; i++ {
		resp.Rows = append(resp.Rows, table.Rows[i].Cells)
	}
	c.JSON(http.StatusOK, resp)
}

// errRollback ends an import that is not to be stored.
var errRollback = errors.New("import rolled back")

// run loads the file. Every row runs in a savepoint of one transaction, so
// a failed row leaves the others to be checked; the transaction commits only
// when no row failed and the run is not a dry run.
func (im *importer[T, PT]) run(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.ImportCatalogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		return
	}
	table, err := readFile(req.FileName, req.Content)
	if err != nil {
		_ = c.Error(err)
		return
	}
	columns, err := im.columns(table.Header, req.Mapping, !req.MatchByCode)
	if err != nil {
		_ = c.Error(err)
		return
	}
	if req.MatchByCode && !slicesContainsField(columns, "code") {
		_ = c.Error(apperror.NewValidation("Чтобы обновлять элементы по коду, сопоставьте колонку с полем «Код»"))
		return
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		_ = c.Error(apperror.NewInternal(err))
		return
	}
	savepoints, ok := txm.(tx.SavepointManager)
	if !ok {
		_ = c.Error(apperror.NewInternal(fmt.Errorf("the transaction manager has no savepoints")))
		return
	}
	resp := dto.ImportCatalogResponse{DryRun: req.DryRun, Errors: []dto.ImportRowError{}}
	refs := map[string]id.ID{}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		for _, row := range table.Rows {
			var rowErr *dto.ImportRowError
			err := savepoints.RunInSavepoint(ctx, func(ctx context.Context) error {
				created, failure := im.importRow(ctx, row, columns, req.MatchByCode, refs)
				if failure != nil {
					rowErr = failure
					return errRollback
				}
				if created {
					resp.Created++
				} else {
					resp.Updated++
				}
				return nil
			})
			if rowErr != nil {
				resp.Errors = append(resp.Errors, *rowErr)
				continue
			}
			if err != nil {
				resp.Errors = append(resp.Errors, dto.ImportRowError{Line: row.Line, Message: rowMessage(err)})
			}
		}
		if len(resp.Errors) > 0 || req.DryRun {
			return errRollback
		}
		return nil
	})
	if err != nil && !errors.Is(err, errRollback) {
		_ = c.Error(err)
		return
	}
	resp.Applied = err == nil
	c.JSON(http.StatusOK, resp)
}

func slicesContainsField(columns []importColumn, name string) bool {
	for _, c := range columns {
		if c.field.name == name {
			return true
		}
	}
	return false
}

// importColumn is a file column and the field it fills.
type importColumn struct {
	index  int
	header string
	field  importField
}

// columns checks the mapping: known headers and fields, each field once,
// and, when every row creates an item, every required field filled; rows
// that update items keep the fields the file does not have.
func (im *importer[T, PT]) columns(header []string, mapping map[string]string, requireAll bool) ([]importColumn, error) {
	index := map[string]int{}
	for i, h := range header {
		index[h] = i
	}
	var columns []importColumn
	used := map[string]string{}
	for h, name := range mapping {
		if name == "" {
			continue
		}
		i, ok := index[h]
		if !ok {
			return nil, apperror.NewValidation(fmt.Sprintf("В файле нет колонки «%s»", h))
		}
		f, ok := im.fieldByName(name)
		if !ok {
			return nil, apperror.NewValidation(fmt.Sprintf("У справочника нет поля %s", name))
		}
		if other, dup := used[name]; dup {
			return nil, apperror.NewValidation(fmt.Sprintf("Поле «%s» заполняют две колонки: «%s» и «%s»", f.label, other, h))
		}
		used[name] = h
		columns = append(columns, importColumn{index: i, header: h, field: f})
	}
	for _, f := range im.fields {
		if requireAll && f.required && used[f.name] == "" {
			return nil, apperror.NewValidation(fmt.Sprintf("Сопоставьте колонку с обязательным полем «%s»", f.label))
		}
	}
	return columns, nil
}

// importRow creates the row's item or, with matchByCode, updates the item
// with its code. It reports whether it created one, or what is wrong.
func (im *importer[T, PT]) importRow(ctx context.Context, row importing.Row, columns []importColumn, matchByCode bool, refs map[string]id.ID) (bool, *dto.ImportRowError) {
	values := map[string]reflect.Value{}
	for _, col := range columns {
		value, err := im.convert(ctx, col.field, row.Cells[col.index], refs)
		if err != nil {
			return false, &dto.ImportRowError{Line: row.Line, Column: col.header, Message: err.Error()}
		}
		values[col.field.field.Name] = value
	}
	fail := func(err error) (bool, *dto.ImportRowError) {
		return false, &dto.ImportRowError{Line: row.Line, Column: columnOf(err, columns), Message: rowMessage(err)}
	}

	var existingID *id.ID
	if code, ok := values["Code"]; ok && matchByCode && code.String() != "" {
		found, err := lookupByCode(ctx, im.def.Table, code.String())
		if err != nil {
			return fail(err)
		}
		existingID = found
	}
	if existingID == nil {
		req := reflect.New(im.shape.create).Elem()
		for name, value := range values {
			req.FieldByName(name).Set(value)
		}
		if err := binding.Validator.ValidateStruct(req.Addr().Interface()); err != nil {
			return fail(err)
		}
		model := PT(newAutoModel[T](req, entity.CatalogKind))
		im.shape.applyAutoRequest(req, model)
		if err := im.service.Create(ctx, model); err != nil {
			return fail(err)
		}
		return true, nil
	}

	existing, err := im.service.GetByID(ctx, *existingID)
	if err != nil {
		return fail(err)
	}
	stored := reflect.ValueOf(existing).Elem()
	req := reflect.New(im.shape.update).Elem()
	for i := range req.NumField() {
		f := req.Type().Field(i)
		if from := stored.FieldByName(f.Name); from.IsValid() && from.Type().AssignableTo(f.Type) {
			req.Field(i).Set(from)
		}
	}
	for name, value := range values {
		req.FieldByName(name).Set(value)
	}
	if err := binding.Validator.ValidateStruct(req.Addr().Interface()); err != nil {
		return fail(err)
	}
	im.shape.applyAutoRequest(req, existing)
	if err := im.service.Update(ctx, existing); err != nil {
		return fail(err)
	}
	return false, nil
}

// convert reads a cell as the field's type; a reference is the code or name
// of an item of the referenced catalog.
func (im *importer[T, PT]) convert(ctx context.Context, f importField, text string, refs map[string]id.ID) (reflect.Value, error) {
	t := f.field.Type
	pointer := t.Kind() == reflect.Pointer
	base := t
	if pointer {
		base = t.Elem()
	}
	if text == "" {
		return reflect.Zero(t), nil
	}
	var value reflect.Value
	switch {
	case base == reflect.TypeFor[id.ID]():
		if f.refTable == "" {
			parsed, err := id.Parse(text)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("не идентификатор: «%s»", text)
			}
			value = reflect.ValueOf(parsed)
			break
		}
		found, err := lookupReference(ctx, f.refTable, text, f.parent, refs)
		if err != nil {
			return reflect.Value{}, err
		}
		value = reflect.ValueOf(found)
	case base == reflect.TypeFor[types.Quantity]():
		q, err := types.ParseQuantity(strings.ReplaceAll(strings.ReplaceAll(text, " ", ""), ",", "."))
		if err != nil {
			return reflect.Value{}, fmt.Errorf("не число: «%s»", text)
		}
		value = reflect.ValueOf(q)
	case base == reflect.TypeFor[types.MinorUnits]():
		n, err := strconv.ParseInt(strings.ReplaceAll(text, " ", ""), 10, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("не целое число минимальных единиц: «%s»", text)
		}
		value = reflect.ValueOf(types.MinorUnits(n))
	case base == reflect.TypeFor[time.Time]():
		parsed, err := parseImportDate(text)
		if err != nil {
			return reflect.Value{}, err
		}
		value = reflect.ValueOf(parsed)
	case base.Kind() == reflect.Bool:
		switch strings.ToLower(text) {
		case "да", "1", "true", "истина", "yes", "+":
			value = reflect.ValueOf(true)
		case "нет", "0", "false", "ложь", "no", "-":
			value = reflect.ValueOf(false)
		default:
			return reflect.Value{}, fmt.Errorf("ожидается «да» или «нет»: «%s»", text)
		}
	case base.Kind() >= reflect.Int && base.Kind() <= reflect.Int64:
		n, err := strconv.ParseInt(strings.ReplaceAll(text, " ", ""), 10, 64)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("не целое число: «%s»", text)
		}
		value = reflect.New(base).Elem()
		value.SetInt(n)
	case base.Kind() == reflect.String:
		value = reflect.New(base).Elem()
		value.SetString(text)
	default:
		return reflect.Value{}, fmt.Errorf("поле этого вида из файла не загружается")
	}
	value = value.Convert(base)
	if pointer {
		ptr := reflect.New(base)
		ptr.Elem().Set(value)
		return ptr, nil
	}
	return value, nil
}

func parseImportDate(text string) (time.Time, error) {
	for _, layout := range []string{"02.01.2006", "2006-01-02", "02.01.2006 15:04", "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, text); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("не дата: «%s» (ожидается ДД.ММ.ГГГГ)", text)
}

// lookupReference finds the item of table whose code or name is text, a
// folder for a parent and an element otherwise.
func lookupReference(ctx context.Context, table, text string, folder bool, cache map[string]id.ID) (id.ID, error) {
	key := table + "\x00" + strings.ToLower(text)
	if found, ok := cache[key]; ok {
		return found, nil
	}
	if !strings.HasPrefix(table, "cat_") {
		return id.ID{}, fmt.Errorf("ссылка на документ из файла не загружается")
	}
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, fmt.Sprintf(
		`SELECT id FROM %s WHERE _deleted_at IS NULL AND NOT deletion_mark AND is_folder = $2
		   AND (lower(code) = lower($1) OR lower(name) = lower($1)) LIMIT 2`, table), text, folder)
	if err != nil {
		return id.ID{}, fmt.Errorf("look up %s: %w", table, err)
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[id.ID])
	if err != nil {
		return id.ID{}, fmt.Errorf("look up %s: %w", table, err)
	}
	switch len(found) {
	case 0:
		if folder {
			return id.ID{}, fmt.Errorf("нет группы «%s»", text)
		}
		return id.ID{}, fmt.Errorf("не найдено «%s»", text)
	case 1:
		cache[key] = found[0]
		return found[0], nil
	default:
		return id.ID{}, fmt.Errorf("«%s» неоднозначно: укажите код", text)
	}
}

// lookupByCode finds the item of table with code; nil when none.
func lookupByCode(ctx context.Context, table, code string) (*id.ID, error) {
	var found id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx,
		fmt.Sprintf(`SELECT id FROM %s WHERE _deleted_at IS NULL AND code = $1`, table), code).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("look up code: %w", err)
	}
	return &found, nil
}

// rowMessage is what the user reads of a row's failure: the business
// message, or a field rule broken.
func rowMessage(err error) string {
	var invalid validator.ValidationErrors
	if errors.As(err, &invalid) && len(invalid) > 0 {
		if invalid[0].Tag() == "required" {
			return "обязательное поле не заполнено"
		}
		return fmt.Sprintf("значение не проходит правило «%s»", invalid[0].Tag())
	}
	if appErr, ok := apperror.AsAppError(err); ok && appErr.Code != apperror.CodeInternal && appErr.Code != apperror.CodeDatabase {
		return appErr.Message
	}
	return "строка не записана"
}

// columnOf is the column of the field a validation failure names.
func columnOf(err error, columns []importColumn) string {
	var invalid validator.ValidationErrors
	if errors.As(err, &invalid) && len(invalid) > 0 {
		for _, c := range columns {
			if c.field.field.Name == invalid[0].StructField() {
				return c.header
			}
		}
	}
	return ""
}
