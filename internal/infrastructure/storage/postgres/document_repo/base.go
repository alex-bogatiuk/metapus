// Package document_repo provides PostgreSQL implementations for document repositories.
// In Database-per-Tenant architecture, TxManager is obtained from context per-request.
package document_repo

import (
	"context"
	"errors"
	"fmt"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/readpolicy"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/cursor"
	"github.com/alex-bogatiuk/metapus/internal/domain/filter"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	pgfilter "github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/filter"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/keyset"
)

func NewDocumentRepoForDefinition[T any](def entity.Definition, selectCols []string, newFn func() T) *BaseDocumentRepo[T] {
	if err := def.Validate(); err != nil {
		panic(err)
	}
	if def.Kind != entity.DocumentKind {
		panic("document repository requires a document definition")
	}
	repo := NewBaseDocumentRepo(def.Table, selectCols, newFn)
	repo.SetEntityName(def.Key)
	if def.Statuses.Declared() && !slices.Contains(selectCols, def.Statuses.Column) {
		panic(fmt.Sprintf("document %q: status column %q is not a column of the document", def.Name, def.Statuses.Column))
	}
	repo.definition = def
	return repo
}

// BaseDocumentRepo provides common CRUD operations for document entities.
// In Database-per-Tenant architecture:
// - TxManager is obtained from context per-request
// - No tenant filtering in queries (isolation is physical)
type BaseDocumentRepo[T any] struct {
	filterFields    map[string]filter.FieldSpec
	tableName       string
	selectCols      []string
	newFn           func() T
	validCols       map[string]struct{}                  // whitelist for advanced filter columns
	orderCols       map[string]struct{}                  // whitelist for ORDER BY columns
	tableParts      map[string]filter.TablePartInfo      // table part name → child table info (for EXISTS subqueries)
	referenceFields map[string]filter.ReferenceFieldInfo // field name → reference catalog info (for deep filtering)

	// entityName is the logical entity name (e.g. "crypto_invoice") used for per-entity RLS.
	entityName string

	// rlsDimensions maps security dimension names to DB column names.
	// Configured via RegisterRLSDimension. At query time, DataScope.ApplyConditions
	// uses this map to inject WHERE conditions for matching dimensions.
	rlsDimensions map[string]string // e.g. {"organization": "organization_id"}

	// definition is the document's Definition (NewDocumentRepoForDefinition):
	// writes check its statuses (entity.StatusModel) and keep its sequences
	// (entity.SequenceRule).
	definition entity.Definition
}

// NewBaseDocumentRepo creates a new base document repository.
// Note: TxManager is obtained from context, not stored in struct.
// Automatically builds validCols and orderCols from selectCols
// plus standard document columns.
func NewBaseDocumentRepo[T any](
	tableName string,
	selectCols []string,
	newFn func() T,
) *BaseDocumentRepo[T] {
	// Standard document columns always valid for filtering
	extraFilterCols := []string{"id", "number", "date", "posted", "deletion_mark"}
	validCols := filter.BuildValidCols(selectCols, extraFilterCols...)

	// Standard document columns always valid for ordering
	extraOrderCols := []string{"id", "number", "date", "created_at", "updated_at", "version"}
	orderCols := filter.BuildOrderCols(selectCols, extraOrderCols...)

	return &BaseDocumentRepo[T]{
		filterFields: postgres.ExtractFilterFields[T](selectCols...),
		tableName:    tableName,
		selectCols:   selectCols,
		newFn:        newFn,
		validCols:    validCols,
		orderCols:    orderCols,
	}
}

// SetEntityName sets the logical entity name for per-entity RLS dimension resolution.
func (r *BaseDocumentRepo[T]) SetEntityName(name string) {
	r.entityName = name
}

// RegisterRLSDimension registers a security dimension for row-level filtering.
// dimensionName is a logical name (e.g., "organization", "counterparty", "cost_article").
// dbColumn is the actual DB column name in this entity's table (e.g., "organization_id").
//
// At query time, if the user's DataScope has allowed values for this dimension,
// a WHERE dbColumn IN (...) condition is added automatically.
func (r *BaseDocumentRepo[T]) RegisterRLSDimension(dimensionName, dbColumn string) {
	if r.rlsDimensions == nil {
		r.rlsDimensions = make(map[string]string)
	}
	r.rlsDimensions[dimensionName] = dbColumn
}

// RegisterTablePart registers a child table (table part / tabular section)
// so that dot-notation filters like "lines.token_id" are translated into
// EXISTS subqueries instead of direct WHERE conditions on the main table.
//
// partName is the snake_case name that arrives from frontend (e.g. "lines").
// childTable comes from entity.Definition.PartTable (e.g. "doc_crypto_sweeps__lines").
// foreignKey is the FK column in the child table (e.g. "document_id").
// columns are the DB column names allowed for filtering.
func (r *BaseDocumentRepo[T]) RegisterTablePart(partName, childTable, foreignKey string, fields map[string]filter.FieldSpec) {
	if r.tableParts == nil {
		r.tableParts = make(map[string]filter.TablePartInfo)
	}
	r.tableParts[partName] = filter.TablePartInfo{
		TableName:  childTable,
		ForeignKey: foreignKey,
		Fields:     fields,
	}
}

// RegisterReferenceField registers a reference field (e.g., counterparty_id)
// so that dot-notation filters like "counterparty_id.inn" are translated into
// EXISTS subqueries on the catalog table.
//
// fieldName is the parent's struct field name in snake_case (e.g. "counterparty_id").
// refTableName is the SQL table name of the catalog (e.g. "cat_counterparties").
// foreignKey is the column name in the primary table (e.g. "counterparty_id").
// columns are the DB column names allowed for filtering in the catalog.
func (r *BaseDocumentRepo[T]) RegisterReferenceField(fieldName, refTableName, foreignKey string, fields map[string]filter.FieldSpec) {
	if r.referenceFields == nil {
		r.referenceFields = make(map[string]filter.ReferenceFieldInfo)
	}
	r.referenceFields[fieldName] = filter.ReferenceFieldInfo{
		TableName:  refTableName,
		ForeignKey: foreignKey,
		Fields:     fields,
	}
}

// GetTxManager returns the TxManager from context (exported for concrete repos).
func (r *BaseDocumentRepo[T]) GetTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// Builder returns a new squirrel builder.
// SelectColumns returns the document columns in table order, for concrete
// repositories that build their own queries.
func (r *BaseDocumentRepo[T]) SelectColumns() []string {
	return r.selectCols
}

func (r *BaseDocumentRepo[T]) Builder() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

// checkPeriodLock refuses a write of a document whose new date, or stored
// date (for a stored document), is in the closed period (settings
// PeriodLockSettings). Every document write goes through the repository,
// the posting engine's included, so one check covers creating, changing,
// posting, unposting and marking a document.
func (r *BaseDocumentRepo[T]) checkPeriodLock(ctx context.Context, entityID any, newDate any) error {
	lock, timezone, err := postgres.PeriodLock(ctx)
	if err != nil || lock.Date == "" {
		return err
	}
	var dates []time.Time
	if d, ok := newDate.(time.Time); ok {
		dates = append(dates, d)
	}
	if entityID != nil {
		var stored time.Time
		err := r.GetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx,
			fmt.Sprintf("SELECT date FROM %s WHERE id = $1", r.tableName), entityID).Scan(&stored)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("read stored date: %w", err)
		}
		dates = append(dates, stored)
	}
	return lock.CheckDocumentDates(ctx, timezone, dates...)
}

// statusChange checks the status a write gives a document of a Definition
// with statuses (entity.StatusModel) and returns it with the stored one; a
// new document's stored status is empty, and one written without a status
// gets the model's initial one. changed reports whether the write changes
// the status, so that its history gets a row.
func (r *BaseDocumentRepo[T]) statusChange(ctx context.Context, doc T, entityID any) (status, stored string, changed bool, err error) {
	model := r.definition.Statuses
	if !model.Declared() {
		return "", "", false, nil
	}
	status, _ = postgres.StringField(doc, model.Column)
	if entityID != nil {
		err := r.GetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx,
			fmt.Sprintf("SELECT %s FROM %s WHERE id = $1", model.Column, r.tableName), entityID).Scan(&stored)
		if err != nil && err != pgx.ErrNoRows {
			return "", "", false, fmt.Errorf("read stored status: %w", err)
		}
	}
	if status == "" {
		status = stored
		if status == "" {
			status = model.Initial
		}
		postgres.SetStringField(doc, model.Column, status)
	}
	if err := domain.CheckStatusChange(ctx, r.definition, stored, status); err != nil {
		return "", "", false, err
	}
	return status, stored, status != stored, nil
}

// Create inserts a new document.
func (r *BaseDocumentRepo[T]) Create(ctx context.Context, entity T) error {
	status, _, changed, err := r.statusChange(ctx, entity, nil)
	if err != nil {
		return err
	}
	data := postgres.StructToMap(entity)
	if len(data) == 0 {
		return fmt.Errorf("no db tags found in entity")
	}
	if err := r.checkPeriodLock(ctx, nil, data["date"]); err != nil {
		return err
	}

	filteredData := make(map[string]any, len(r.selectCols))
	for _, col := range r.selectCols {
		if val, ok := data[col]; ok {
			filteredData[col] = val
		}
	}

	q := r.Builder().
		Insert(r.tableName).
		SetMap(filteredData)

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build insert: %w", err)
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	_, err = querier.Exec(ctx, sql, args...)
	if err != nil {
		if postgres.IsForeignKeyViolation(err) {
			field := postgres.ExtractForeignKeyField(err, r.tableName)
			return apperror.NewBusinessRule("INVALID_REFERENCE", "Связанный элемент удален. Выберите другой.").
				WithDetail("field", field)
		}
		if postgres.UniqueViolationOn(err, "number") {
			return duplicateNumber(err)
		}
		return fmt.Errorf("insert %s: %w", r.tableName, err)
	}
	if changed {
		if err := postgres.RecordDocumentStatus(ctx, r.definition.Key, data["id"], status, ""); err != nil {
			return err
		}
	}
	return r.keepSequences(ctx, entity, data, nil)
}

// keepSequences keeps the boundaries of the document's sequences after a
// write (postgres.KeepSequences); stored is the document as it was before
// an update, nil for a new one.
func (r *BaseDocumentRepo[T]) keepSequences(ctx context.Context, doc T, data map[string]any, stored *storedForSequence) error {
	if !r.definition.Sequence.Declared() {
		return nil
	}
	write := postgres.SequenceWrite{}
	write.Date, _ = data["date"].(time.Time)
	write.Posted, _ = data["posted"].(bool)
	if stored != nil {
		write.Stored, write.StoredDate, write.StoredPosted = true, stored.date, stored.posted
	}
	return postgres.KeepSequences(ctx, r.definition, any(doc), write)
}

// storedForSequence is a stored document as its sequences see it.
type storedForSequence struct {
	date   time.Time
	posted bool
}

// storedSequenceState reads the stored date and posted flag of a document
// in a sequence; nil for a document that is not in one.
func (r *BaseDocumentRepo[T]) storedSequenceState(ctx context.Context, entityID any) (*storedForSequence, error) {
	if !r.definition.Sequence.Declared() {
		return nil, nil
	}
	var stored storedForSequence
	err := r.GetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx,
		fmt.Sprintf("SELECT date, posted FROM %s WHERE id = $1", r.tableName), entityID).Scan(&stored.date, &stored.posted)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read stored document: %w", err)
	}
	return &stored, nil
}

// duplicateNumber is the error of a write whose number another document holds.
func duplicateNumber(err error) error {
	return apperror.NewConflict("Номер уже занят другим документом").WithDetail("field", "number").WithCause(err)
}

// Update updates an existing document with optimistic locking.
func (r *BaseDocumentRepo[T]) Update(ctx context.Context, entity T) error {
	data := postgres.StructToMap(entity)
	if len(data) == 0 {
		return fmt.Errorf("no db tags found in entity")
	}

	entityID, ok := data["id"]
	if !ok {
		return fmt.Errorf("entity has no 'id' field")
	}

	version, ok := data["version"].(int)
	if !ok {
		return fmt.Errorf("entity has no 'version' field or it is not an int")
	}
	if err := r.checkPeriodLock(ctx, entityID, data["date"]); err != nil {
		return err
	}
	status, stored, changed, err := r.statusChange(ctx, entity, entityID)
	if err != nil {
		return err
	}
	storedSequence, err := r.storedSequenceState(ctx, entityID)
	if err != nil {
		return err
	}
	if r.definition.Statuses.Declared() {
		data[r.definition.Statuses.Column] = status
	}

	// Exclude immutable fields
	filteredData := make(map[string]any, len(r.selectCols))
	for _, col := range r.selectCols {
		if col == "id" || col == "created_at" || col == "created_by" {
			continue
		}
		if col == "version" || col == "updated_at" {
			continue // version/updated_at are managed by repo
		}
		if val, ok := data[col]; ok {
			filteredData[col] = val
		}
	}

	q := r.Builder().
		Update(r.tableName).
		SetMap(filteredData).
		Set("version", squirrel.Expr("version + 1")).
		Set("updated_at", squirrel.Expr("NOW()")).
		Where(squirrel.Eq{"id": entityID}).
		Where(squirrel.Eq{"version": version}).
		Suffix("RETURNING version, updated_at")

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build update: %w", err)
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)

	var newVersion int
	var updatedAt time.Time

	err = querier.QueryRow(ctx, sql, args...).Scan(&newVersion, &updatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return apperror.NewConcurrentModification(r.tableName, entityID)
		}
		if postgres.IsForeignKeyViolation(err) {
			field := postgres.ExtractForeignKeyField(err, r.tableName)
			return apperror.NewBusinessRule("INVALID_REFERENCE", "Связанный элемент удален. Выберите другой.").
				WithDetail("field", field)
		}
		if postgres.UniqueViolationOn(err, "number") {
			return duplicateNumber(err)
		}
		return fmt.Errorf("update %s: %w", r.tableName, err)
	}

	// Update entity in memory to prevent stale object issues
	if v, ok := any(entity).(interface{ SetVersion(int) }); ok {
		v.SetVersion(newVersion)
	}
	if v, ok := any(entity).(interface{ SetUpdatedAt(time.Time) }); ok {
		v.SetUpdatedAt(updatedAt)
	}
	if changed {
		if err := postgres.RecordDocumentStatus(ctx, r.definition.Key, entityID, status, stored); err != nil {
			return err
		}
	}
	if storedSequence != nil {
		return r.keepSequences(ctx, entity, data, storedSequence)
	}
	return nil
}

// Delete soft-deletes a document.
func (r *BaseDocumentRepo[T]) Delete(ctx context.Context, entityID id.ID) error {
	if err := r.checkPeriodLock(ctx, entityID, nil); err != nil {
		return err
	}
	q := r.Builder().
		Update(r.tableName).
		Set("deletion_mark", true).
		Set("updated_at", squirrel.Expr("NOW()")).
		Set("version", squirrel.Expr("version + 1")).
		Where(squirrel.Eq{"id": entityID})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	result, err := querier.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("delete %s: %w", r.tableName, err)
	}

	if result.RowsAffected() == 0 {
		return apperror.NewNotFound(r.tableName, entityID.String())
	}

	return nil
}

// baseSelect creates a SELECT builder.
func (r *BaseDocumentRepo[T]) baseSelect(ctx context.Context) squirrel.SelectBuilder {
	return r.Builder().
		Select(r.selectCols...).
		From(r.tableName)
}

// GetByID retrieves a document by ID.
func (r *BaseDocumentRepo[T]) GetByID(ctx context.Context, entityID id.ID) (T, error) {
	entity := r.newFn()
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"id": entityID})

	sql, args, err := q.ToSql()
	if err != nil {
		return entity, fmt.Errorf("build query: %w", err)
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, entity, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return entity, apperror.NewNotFound(r.tableName, entityID.String())
		}
		return entity, fmt.Errorf("get by id: %w", err)
	}

	return entity, nil
}

// GetByIDForUpdate retrieves a document by ID and locks the row.
func (r *BaseDocumentRepo[T]) GetByIDForUpdate(ctx context.Context, entityID id.ID) (T, error) {
	entity := r.newFn()
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"id": entityID}).
		Suffix("FOR UPDATE")

	sql, args, err := q.ToSql()
	if err != nil {
		return entity, fmt.Errorf("build query: %w", err)
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, entity, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return entity, apperror.NewNotFound(r.tableName, entityID.String())
		}
		return entity, fmt.Errorf("get by id for update: %w", err)
	}

	return entity, nil
}

// GetByNumber retrieves a document by Number.
func (r *BaseDocumentRepo[T]) GetByNumber(ctx context.Context, number string) (T, error) {
	entity := r.newFn()
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"number": number})

	sql, args, err := q.ToSql()
	if err != nil {
		return entity, fmt.Errorf("build query: %w", err)
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, entity, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return entity, apperror.NewNotFound(r.tableName, number)
		}
		return entity, fmt.Errorf("get by number: %w", err)
	}

	return entity, nil
}

// buildWhereConditions builds WHERE conditions from domain.ListFilter.
// Handles standard filters (search, deletion_mark), RLS DataScope, and advanced filters.
func (r *BaseDocumentRepo[T]) buildWhereConditions(f domain.ListFilter) ([]squirrel.Sqlizer, error) {
	conditions := make([]squirrel.Sqlizer, 0, 8)

	if !f.IncludeDeleted {
		conditions = append(conditions, squirrel.Eq{"deletion_mark": false})
	}

	if f.Search != "" {
		// M5: 1С-style fuzzy search on document number
		if cond := pgfilter.BuildSearchConditions(f.Search, []string{"number"}); cond != nil {
			conditions = append(conditions, cond)
		}
	}

	// Apply RLS DataScope conditions
	if f.DataScope != nil && len(r.rlsDimensions) > 0 {
		rlsConditions := readpolicy.ApplyConditions(f.DataScope, r.entityName, r.rlsDimensions)
		conditions = append(conditions, rlsConditions...)
	}

	// Apply advanced filters — extract __search pseudo-field first (M5)
	advFilters := f.AdvancedFilters
	if searchQuery, remaining := filter.ExtractSearchQuery(advFilters); searchQuery != "" {
		// __search from AdvancedFilters is applied only if f.Search was empty
		// to prevent duplicate conditions when both are present
		if f.Search == "" {
			if cond := pgfilter.BuildSearchConditions(searchQuery, []string{"number"}); cond != nil {
				conditions = append(conditions, cond)
			}
		}
		advFilters = remaining
	}

	if len(advFilters) > 0 {
		headerFilters := make([]filter.Item, 0, len(advFilters))
		for _, item := range advFilters {
			if strings.Contains(item.Field, ".") {
				parts := strings.SplitN(item.Field, ".", 2)
				prefix, subfield := parts[0], parts[1]

				// 1. Check if prefix is a table part
				if tp, ok := r.tableParts[prefix]; ok {
					cond, err := pgfilter.BuildTablePartCondition(item, r.tableName, tp, subfield)
					if err != nil {
						return nil, err
					}
					conditions = append(conditions, cond)
					continue
				}

				// 2. Check if prefix is a reference field
				if ref, ok := r.referenceFields[prefix]; ok {
					cond, err := pgfilter.BuildReferenceFieldCondition(item, r.tableName, ref, subfield)
					if err != nil {
						return nil, err
					}
					conditions = append(conditions, cond)
					continue
				}

				return nil, fmt.Errorf("invalid dot-notation field: %s", item.Field)
			} else {
				headerFilters = append(headerFilters, item)
			}
		}
		if len(headerFilters) > 0 {
			advConditions, err := pgfilter.BuildConditions(headerFilters, r.filterFields, r.tableName)
			if err != nil {
				return nil, err
			}
			conditions = append(conditions, advConditions...)
		}
	}

	return conditions, nil
}

// List retrieves documents with cursor-based (keyset) pagination.
func (r *BaseDocumentRepo[T]) List(ctx context.Context, f domain.ListFilter) (domain.CursorListResult[T], error) {
	var result domain.CursorListResult[T]

	// Parse sort spec (documents default to date DESC)
	spec, err := keyset.ParseOrderBy(f.OrderBy, "date", "DESC", r.orderCols)
	if err != nil {
		return result, err
	}

	spec, err = postgres.ResolveSortSpec(ctx, r.GetTxManager(ctx).GetQuerier(ctx), r.tableName, spec)
	if err != nil {
		return result, err
	}

	// Build WHERE conditions
	conditions, err := r.buildWhereConditions(f)
	if err != nil {
		return result, err
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	// Dispatch based on cursor direction
	if f.CursorReq != nil {
		switch f.CursorReq.Direction {
		case cursor.DirAround:
			return r.listAround(ctx, f, spec, conditions, limit)
		case cursor.DirAfter:
			return r.listForward(ctx, spec, conditions, f.CursorReq.Token, limit, true)
		case cursor.DirBefore:
			return r.listBackward(ctx, spec, conditions, f.CursorReq.Token, limit)
		}
	}

	// First page (no cursor) — count total (unless skipped) + forward from start
	if !f.SkipCount {
		baseQ := r.baseSelect(ctx)
		for _, cond := range conditions {
			baseQ = baseQ.Where(cond)
		}
		countQ := r.Builder().Select("COUNT(*)").FromSelect(baseQ, "sub")
		countSQL, countArgs, err := countQ.ToSql()
		if err != nil {
			return result, fmt.Errorf("build count: %w", err)
		}
		var total int64
		if err := querier.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
			return result, fmt.Errorf("count: %w", err)
		}
		result.TotalCount = &total
	}

	// Fetch limit+1 rows to detect hasMore
	q := r.baseSelect(ctx)
	for _, cond := range conditions {
		q = q.Where(cond)
	}
	q = q.Column(spec.Field + "::text AS __cursor_sort").OrderBy(spec.OrderByClause()).Limit(uint64(limit + 1))

	sql, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("build query: %w", err)
	}
	var pageCursors map[id.ID]string
	result.Items, pageCursors, err = postgres.ScanCursorPage(ctx, querier, r.newFn, spec, sql, args...)
	if err != nil {
		return result, fmt.Errorf("list: %w", err)
	}

	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.HasMore = true
	}
	result.HasPrev = false

	if len(result.Items) > 0 {
		if err := postgres.ApplyPageCursors(&result, pageCursors); err != nil {
			return result, err
		}
	}
	return result, nil
}

// listForward fetches items after a cursor (scroll down).
func (r *BaseDocumentRepo[T]) listForward(ctx context.Context, spec keyset.SortSpec, conditions []squirrel.Sqlizer, token string, limit int, setPrev bool) (domain.CursorListResult[T], error) {
	var result domain.CursorListResult[T]

	values, err := keyset.DecodeCursor(token, spec)
	if err != nil {
		return result, err
	}

	q := r.baseSelect(ctx)
	for _, cond := range conditions {
		q = q.Where(cond)
	}
	q = q.Where(keyset.TupleCondition(spec, values, true))
	q = q.Column(spec.Field + "::text AS __cursor_sort").OrderBy(spec.OrderByClause()).Limit(uint64(limit + 1))

	sql, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("build forward query: %w", err)
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	var pageCursors map[id.ID]string
	result.Items, pageCursors, err = postgres.ScanCursorPage(ctx, querier, r.newFn, spec, sql, args...)
	if err != nil {
		return result, fmt.Errorf("list forward: %w", err)
	}

	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.HasMore = true
	}
	result.HasPrev = setPrev

	if len(result.Items) > 0 {
		if err := postgres.ApplyPageCursors(&result, pageCursors); err != nil {
			return result, err
		}
	}
	return result, nil
}

// listBackward fetches items before a cursor (scroll up).
func (r *BaseDocumentRepo[T]) listBackward(ctx context.Context, spec keyset.SortSpec, conditions []squirrel.Sqlizer, token string, limit int) (domain.CursorListResult[T], error) {
	var result domain.CursorListResult[T]

	values, err := keyset.DecodeCursor(token, spec)
	if err != nil {
		return result, err
	}

	q := r.baseSelect(ctx)
	for _, cond := range conditions {
		q = q.Where(cond)
	}
	q = q.Where(keyset.TupleCondition(spec, values, false))
	q = q.Column(spec.Field + "::text AS __cursor_sort").OrderBy(spec.InvertedOrderByClause()).Limit(uint64(limit + 1))

	sql, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("build backward query: %w", err)
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	var pageCursors map[id.ID]string
	result.Items, pageCursors, err = postgres.ScanCursorPage(ctx, querier, r.newFn, spec, sql, args...)
	if err != nil {
		return result, fmt.Errorf("list backward: %w", err)
	}

	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.HasPrev = true
	}
	result.HasMore = true

	reverseSlice(result.Items)

	if len(result.Items) > 0 {
		if err := postgres.ApplyPageCursors(&result, pageCursors); err != nil {
			return result, err
		}
	}
	return result, nil
}

// listAround fetches items around a target ID (teleportation / "show in list").
func (r *BaseDocumentRepo[T]) listAround(ctx context.Context, f domain.ListFilter, spec keyset.SortSpec, conditions []squirrel.Sqlizer, limit int) (domain.CursorListResult[T], error) {
	var result domain.CursorListResult[T]
	half := max(limit/2, 1)

	targetID, err := id.Parse(f.CursorReq.TargetID)
	if err != nil {
		return result, apperror.NewValidation("invalid around target ID")
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	var sortValue *string
	lookup := r.Builder().Select(spec.Field + "::text").From(r.tableName).Where(squirrel.Eq{"id": targetID})
	for _, condition := range conditions {
		lookup = lookup.Where(condition)
	}
	lookupSQL, lookupArgs, err := lookup.ToSql()
	if err != nil {
		return result, err
	}
	if err := querier.QueryRow(ctx, lookupSQL, lookupArgs...).Scan(&sortValue); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, apperror.NewNotFound(r.tableName, targetID.String())
		}
		return result, fmt.Errorf("read around boundary: %w", err)
	}

	targetValues := []any{sortValue, targetID}

	// Fetch rows <= target in inverted order (includes target)
	qBefore := r.baseSelect(ctx)
	for _, cond := range conditions {
		qBefore = qBefore.Where(cond)
	}
	qBefore = qBefore.Where(keyset.TupleConditionInclusive(spec, targetValues, false))
	qBefore = qBefore.Column(spec.Field + "::text AS __cursor_sort").OrderBy(spec.InvertedOrderByClause()).Limit(uint64(half + 1))

	sqlBefore, argsBefore, err := qBefore.ToSql()
	if err != nil {
		return result, fmt.Errorf("build around-before query: %w", err)
	}
	beforeItems, pageCursors, err := postgres.ScanCursorPage(ctx, querier, r.newFn, spec, sqlBefore, argsBefore...)
	if err != nil {
		return result, fmt.Errorf("around before: %w", err)
	}

	hasPrev := false
	if len(beforeItems) > half {
		beforeItems = beforeItems[:half]
		hasPrev = true
	}
	reverseSlice(beforeItems)

	// Fetch rows after target (exclusive)
	qAfter := r.baseSelect(ctx)
	for _, cond := range conditions {
		qAfter = qAfter.Where(cond)
	}
	qAfter = qAfter.Where(keyset.TupleCondition(spec, targetValues, true))
	qAfter = qAfter.Column(spec.Field + "::text AS __cursor_sort").OrderBy(spec.OrderByClause()).Limit(uint64(half + 1))

	sqlAfter, argsAfter, err := qAfter.ToSql()
	if err != nil {
		return result, fmt.Errorf("build around-after query: %w", err)
	}
	afterItems, afterCursors, err := postgres.ScanCursorPage(ctx, querier, r.newFn, spec, sqlAfter, argsAfter...)
	if err != nil {
		return result, fmt.Errorf("around after: %w", err)
	}

	for uid, token := range afterCursors {
		pageCursors[uid] = token
	}
	hasMore := false
	if len(afterItems) > half {
		afterItems = afterItems[:half]
		hasMore = true
	}

	result.Items = make([]T, 0, len(beforeItems)+len(afterItems))
	result.Items = append(result.Items, beforeItems...)

	targetIdx := len(beforeItems) - 1
	if targetIdx >= 0 {
		result.TargetIndex = &targetIdx
	}

	result.Items = append(result.Items, afterItems...)
	result.HasPrev = hasPrev
	result.HasMore = hasMore

	if len(result.Items) > 0 {
		if err := postgres.ApplyPageCursors(&result, pageCursors); err != nil {
			return result, err
		}
	}
	return result, nil
}

func reverseSlice[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// ListIDs returns all document IDs matching the filter (no pagination, no full data).
// SELECT id only — no JOINs, no full rows. Used for filter-based batch operations.
// maxIDs is a safety limit: if more rows match, returns apperror.
func (r *BaseDocumentRepo[T]) ListIDs(ctx context.Context, f domain.ListFilter, maxIDs int) ([]id.ID, error) {
	conditions, err := r.buildWhereConditions(f)
	if err != nil {
		return nil, err
	}

	// SELECT id with safety limit (maxIDs + 1 to detect overflow)
	q := r.Builder().
		Select("id").
		From(r.tableName)
	for _, cond := range conditions {
		q = q.Where(cond)
	}
	q = q.Limit(uint64(maxIDs + 1))

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list_ids query: %w", err)
	}

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	rows, err := querier.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list_ids query: %w", err)
	}
	defer rows.Close()

	ids := make([]id.ID, 0, min(maxIDs, 1000))
	for rows.Next() {
		var docID id.ID
		if err := rows.Scan(&docID); err != nil {
			return nil, fmt.Errorf("list_ids scan: %w", err)
		}
		ids = append(ids, docID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list_ids rows: %w", err)
	}

	if len(ids) > maxIDs {
		return nil, apperror.NewBusinessRule(
			"TOO_MANY_RESULTS",
			fmt.Sprintf("Filter matches more than %d documents. Narrow your selection.", maxIDs),
		).WithDetail("limit", strconv.Itoa(maxIDs))
	}

	return ids, nil
}
