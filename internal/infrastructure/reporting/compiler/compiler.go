// Package compiler implements the Metapus Query Engine — a metadata-driven
// SQL query builder that translates frontend QueryRequest into optimized SQL.
//
// Architecture:
//
//	Frontend (QueryRequest JSON)
//	    ↓
//	Compiler.Execute()
//	    ├── Validate field paths against metadata graph (whitelist)
//	    ├── Resolve reference paths → JoinSteps (auto-discovery)
//	    ├── Build squirrel.SelectBuilder (or delegate to DatasetExecutor)
//	    ├── Append LEFT JOINs for dereferenced fields
//	    ├── Apply WHERE, GROUP BY, ORDER BY, LIMIT
//	    └── Execute SQL → scan into []map[string]interface{}
//	    ↓
//	QueryResult (JSON to frontend)
package compiler

import (
	"context"
	"encoding/hex"
	"fmt"
	"metapus/internal/infrastructure/readpolicy"
	"reflect"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
	"strconv"

	"metapus/internal/application/readaccess"
	"metapus/internal/core/apperror"
	"metapus/internal/core/types"
	"metapus/internal/domain/filter"
	"metapus/internal/domain/reports/schema"
	"metapus/internal/infrastructure/storage/postgres"
	pgfilter "metapus/internal/infrastructure/storage/postgres/filter"
	"metapus/internal/metadata"
)

// MaxJoinDepth is the maximum depth for reference field resolution.
// Paths like "token_id.blockchain_network_id.name" have depth 2.
const MaxJoinDepth = 3

// QueryRequest is the JSON body sent by the frontend to execute a report.
type QueryRequest struct {
	// DisplayFields are grouping/sorting keys validated before SQL, supplied by the grouped handler.
	DisplayFields []string `json:"-"`
	// Dataset is the dataset key, e.g. "document-journal".
	Dataset string `json:"dataset"`

	// Select lists field paths to include in the result.
	// Simple fields: "total_amount", "merchant_id"
	// Dereferenced: "merchant_id.name", "token_id.network_id.name"
	// If empty, all non-hidden fields are selected.
	Select []string `json:"select,omitempty"`

	// GroupBy lists field paths to group by.
	GroupBy []string `json:"groupBy,omitempty"`

	// OrderBy is the field path for sorting.
	OrderBy string `json:"orderBy,omitempty"`

	// OrderDir is "asc" or "desc". Defaults to "asc".
	OrderDir string `json:"orderDir,omitempty"`

	// Filters contains dataset-level parameter values (used by Executors to shape CTEs).
	// E.g. {"merchant_id": ["uuid1"], "date": "2026-01-01"}
	Filters map[string]any `json:"filters,omitempty"`

	// AdvancedFilters are typed filter conditions from FilterSidebar.
	// Each item's Field can be a dot-path (e.g. "token_id.network_id.name"),
	// resolved via the path resolver with automatic JOIN generation.
	// All conditions are compiled into SQL WHERE clauses (push-down).
	AdvancedFilters []filter.Item `json:"advancedFilters,omitempty"`

	// Limit caps the number of rows returned.
	Limit int `json:"limit,omitzero"`

	// Offset for pagination.
	Offset int `json:"offset,omitzero"`

	// ExportColumns is an ordered list of column keys for export.
	// Determines the column order and visibility in CSV/XLSX output.
	// If empty, the default meta.Columns order is used (visible only).
	// Sent by the frontend from the user's current column configuration.
	ExportColumns []string `json:"exportColumns,omitempty"`

	// ExportGroupBy is an ordered list of group keys for export.
	// Used to trigger Control Breaks and insert Subtotals during streaming.
	// The backend will automatically prepend these to the SQL ORDER BY.
	ExportGroupBy []string `json:"exportGroupBy,omitempty"`
}

// QueryResult is the response returned to the frontend.
type QueryResult struct {
	Items      []map[string]any `json:"items"`
	TotalItems int              `json:"totalItems"`
}

// Compiler is the core Query Engine.
// Thread-safe after construction — all mutable state is per-request.
type Compiler struct {
	reads     *readaccess.Service
	registry  *metadata.Registry
	datasets  map[string]*schema.Dataset
	executors map[string]readpolicy.DatasetExecutor
	builder   squirrel.StatementBuilderType
}

// NewCompiler creates a Compiler with the given metadata registry and datasets.
func NewCompiler(reg *metadata.Registry, datasets []*schema.Dataset, reads *readaccess.Service, bindings ...map[string]readpolicy.DatasetExecutor) *Compiler {
	dsMap := make(map[string]*schema.Dataset, len(datasets))
	for _, ds := range datasets {
		dsMap[ds.Key] = ds
	}
	executors := make(map[string]readpolicy.DatasetExecutor)
	for _, binding := range bindings {
		for key, executor := range binding {
			executors[key] = executor
		}
	}
	return &Compiler{
		registry:  reg,
		reads:     reads,
		datasets:  dsMap,
		executors: executors,
		builder:   squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question),
	}
}

// GetDataset returns a dataset by key, or nil.
func (c *Compiler) GetDataset(key string) *schema.Dataset {
	return c.datasets[key]
}

// AllDatasets returns all registered datasets.
func (c *Compiler) AllDatasets() []*schema.Dataset {
	result := make([]*schema.Dataset, 0, len(c.datasets))
	for _, ds := range c.datasets {
		result = append(result, ds)
	}
	return result
}

// BuildQuery authorizes every source and field before composing SQL. It never executes SQL.
func (c *Compiler) BuildQuery(ctx context.Context, req QueryRequest) (squirrel.SelectBuilder, error) {
	ds, ok := c.datasets[req.Dataset]
	if !ok {
		return squirrel.SelectBuilder{}, apperror.NewNotFound("dataset", req.Dataset)
	}

	if c.reads == nil {
		return squirrel.SelectBuilder{}, apperror.NewForbidden("report authorization service is unavailable")
	}
	access, err := readpolicy.AuthorizeReport(ctx, c.reads, ds)
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	if err := c.validateParameters(ctx, ds, req.Filters); err != nil {
		return squirrel.SelectBuilder{}, err
	}

	// 1. Determine selected fields (default to all non-hidden)
	selectPaths := req.Select
	if len(selectPaths) == 0 {
		selectPaths = ds.DefaultSelectedFields()
	}

	// Fields requested by export/grouped views must be present in their data.
	selected := make(map[string]bool)
	selectPaths = append([]string(nil), selectPaths...)
	for _, path := range selectPaths {
		selected[path] = true
	}
	for _, paths := range [][]string{req.ExportColumns, req.ExportGroupBy, req.DisplayFields} {
		for _, path := range paths {
			path = strings.ReplaceAll(path, "__", ".")
			if !selected[path] {
				selectPaths = append(selectPaths, path)
				selected[path] = true
			}
		}
	}
	// Monetary units are mandatory authorized projections, even if the UI hides
	// their columns. Grouped SQL must keep separate units as well.
	selectPaths, err = moneyProjectionFields(ds, selectPaths)
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}

	// 2. Resolve all field paths → collect JoinSteps and SELECT expressions
	resolver := newResolver(c.registry, ds, MaxJoinDepth)
	resolver.authorizeField = func(entity, field string) error { return c.reads.RequireFields(ctx, entity, field) }
	resolver.authorizeDatasetField = func(field *schema.Field) error {
		if len(field.Access) == 0 {
			return apperror.NewForbidden("report field policy is not registered")
		}
		return c.reads.RequireReportFields(ctx, field.Access)
	}
	paths := append(append(append([]string{}, req.ExportColumns...), req.DisplayFields...), req.ExportGroupBy...)
	for _, path := range paths {
		if _, _, err := resolver.ResolveForWhere(strings.ReplaceAll(path, "__", ".")); err != nil {
			return squirrel.SelectBuilder{}, reportPathError("invalid report field", path, err)
		}
	}

	grouping, err := newQueryGrouping(resolver, req.GroupBy, selectPaths)
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	selectExprs := make([]string, 0, len(selectPaths))
	for _, path := range selectPaths {
		expr, err := grouping.expression(path, true)
		if err != nil {
			return squirrel.SelectBuilder{}, reportPathError("invalid report field", path, err)
		}
		selectExprs = append(selectExprs, expr)
	}

	// 3. Build the base query
	var qb squirrel.SelectBuilder

	if ds.ExecutorKey != "" {
		executor, exists := c.executors[ds.ExecutorKey]
		if !exists || executor == nil {
			return squirrel.SelectBuilder{}, apperror.NewInternal(fmt.Errorf("dataset executor %q is not registered", ds.ExecutorKey))
		}
		// Complex dataset — delegate to custom executor
		var err error
		qb, err = executor.BuildQuery(ctx, req.Filters, access)
		if err != nil {
			return squirrel.SelectBuilder{}, fmt.Errorf("dataset executor %q: %w", ds.Key, err)
		}
	} else {
		// Simple dataset — SELECT from BaseTable
		var err error
		qb, err = access.Source("base")
		if err != nil {
			return squirrel.SelectBuilder{}, err
		}
		qb, err = c.applySimpleFilters(qb, ds, req.Filters)
		if err != nil {
			return squirrel.SelectBuilder{}, err
		}
	}

	// 4. Set SELECT columns
	qb = qb.Columns(selectExprs...)

	// 5. Append LEFT JOINs from resolver
	// 5b. Apply advanced filters (from FilterSidebar) — must happen BEFORE
	// appending JOINs, because filter resolution may register additional JOINs.
	if len(req.AdvancedFilters) > 0 {
		var err error
		qb, err = c.applyAdvancedFilters(qb, resolver, req.AdvancedFilters)
		if err != nil {
			return squirrel.SelectBuilder{}, err
		}
	}

	// 7. ORDER BY
	// If ExportGroupBy is present, these keys MUST be the primary sort keys (ASC)
	// so that streaming control breaks can detect group changes.
	if len(req.ExportGroupBy) > 0 {
		for _, gPath := range req.ExportGroupBy {
			// Frontend sends display keys with __, resolver expects dot notation
			sqlPath := strings.ReplaceAll(gPath, "__", ".")
			orderExpr, err := grouping.expression(sqlPath, false)
			if err != nil {
				return squirrel.SelectBuilder{}, reportPathError("invalid report export grouping", gPath, err)
			}
			// Groups are always sorted ascending to keep hierarchy stable
			qb = qb.OrderBy(orderExpr + " ASC")
		}
	}

	if req.OrderBy != "" {
		orderExpr, err := grouping.expression(req.OrderBy, false)
		if err != nil {
			return squirrel.SelectBuilder{}, reportPathError("invalid report ordering", req.OrderBy, err)
		}
		dir := "ASC"
		if strings.EqualFold(req.OrderDir, "desc") {
			dir = "DESC"
		}
		qb = qb.OrderBy(orderExpr + " " + dir)
	} else if ds.DefaultSort != nil && !grouping.enabled {
		dir := "ASC"
		if strings.EqualFold(ds.DefaultSort.Direction, "desc") {
			dir = "DESC"
		}
		orderExpr, err := resolver.ResolveForOrderBy(ds.DefaultSort.Column)
		if err != nil {
			return squirrel.SelectBuilder{}, reportPathError("invalid default ordering", ds.DefaultSort.Column, err)
		}
		qb = qb.OrderBy(orderExpr + " " + dir)
	}

	if grouping.enabled {
		qb = qb.GroupBy(grouping.expressions...)
	}

	// Resolve every SELECT/filter/group/order/export path before collecting joins.
	for _, join := range resolver.Joins() {
		predicate, err := readpolicy.SQLConstraints(ctx, c.reads, join.Entity, join.Alias)
		if err != nil {
			return squirrel.SelectBuilder{}, err
		}
		clause := fmt.Sprintf("LEFT JOIN %s AS %s ON %s.%s = %s.id AND ?", join.Table, join.Alias, join.ParentAlias, join.JoinKey, join.Alias)
		qb = qb.JoinClause(squirrel.Expr(clause, predicate))
	}

	// 8. LIMIT / OFFSET
	if req.Limit > 0 {
		qb = qb.Limit(uint64(req.Limit))
	}
	if req.Offset > 0 {
		qb = qb.Offset(uint64(req.Offset))
	}

	return qb.PlaceholderFormat(squirrel.Dollar), nil
}

// Execute only obtains a database connection after all policies compile.
func (c *Compiler) Execute(ctx context.Context, req QueryRequest) (*QueryResult, error) {
	qb, err := c.BuildQuery(ctx, req)
	if err != nil {
		return nil, err
	}

	// Resolve selected output types before acquiring a database connection.
	ds := c.datasets[req.Dataset]
	outputs := newResolver(c.registry, ds, MaxJoinDepth)
	paths := req.Select
	if len(paths) == 0 {
		paths = ds.DefaultSelectedFields()
	}
	paths = append(append(append(append([]string{}, paths...), req.ExportColumns...), req.ExportGroupBy...), req.DisplayFields...)
	for _, path := range paths {
		path = strings.ReplaceAll(path, "__", ".")
		if _, err := outputs.Resolve(path); err != nil {
			return nil, apperror.NewValidation("invalid report output").WithCause(err)
		}
	}
	// 9. Execute
	query, args, err := qb.ToSql()
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("build report SQL: %w", err))
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, query, args...)
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("execute report query: %w", err))
	}
	defer rows.Close()

	// Manual row scanning: converts pgx types (UUID [16]byte, etc.) to JSON-friendly values.
	items := make([]map[string]any, 0)
	fieldDescs := rows.FieldDescriptions()
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, apperror.NewInternal(fmt.Errorf("scan report row values: %w", err))
		}

		row := make(map[string]any, len(fieldDescs))
		for i, fd := range fieldDescs {
			key := string(fd.Name)
			val := values[i]
			if outputs.moneyOutputs[key] && val != nil {
				amount, err := normalizeMoney(val)
				if outputs.rawOutputs[key] {
					amount, err = normalizeRawMoney(val)
				}
				if err != nil {
					return nil, apperror.NewValidation("report amount violates its registered units").WithDetail("field", key).WithCause(err)
				}
				row[key] = amount
			} else {
				row[key] = normalizeValue(val)
			}
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("iterate report rows: %w", err))
	}

	return &QueryResult{
		Items:      items,
		TotalItems: len(items),
	}, nil
}

// applyAdvancedFilters compiles typed filter conditions (from FilterSidebar)
// into SQL WHERE clauses. Each filter's Field is resolved via the path resolver,
// which validates the path against metadata and registers necessary JOINs.
//
// This enables filtering by nested reference fields:
//
//	"token_id.network_id.name" → LEFT JOIN + WHERE j2.name ILIKE '%...'
//
// All filtering happens at the SQL level (push-down) — zero post-fetch filtering.
func (c *Compiler) applyAdvancedFilters(
	qb squirrel.SelectBuilder,
	r *resolver,
	items []filter.Item,
) (squirrel.SelectBuilder, error) {
	if err := filter.ValidateItems(items); err != nil {
		return qb, apperror.NewValidation("invalid report filter").WithCause(err)
	}

	for _, item := range items {
		// Resolve the field path → SQL expression + register JOINs
		sqlExpr, spec, err := r.ResolveForWhere(item.Field)
		if err != nil {
			return qb, reportPathError("invalid report filter field", item.Field, err)
		}

		resolved, err := filter.Resolve(item, spec)
		if err != nil {
			return qb, apperror.NewValidation("invalid report filter").WithDetail("field", item.Field).WithCause(err)
		}

		// Delegate operator→SQL to the shared filter package
		cond, err := pgfilter.BuildSingleCondition(resolved, sqlExpr)
		if err != nil {
			return qb, apperror.NewValidation("invalid report filter").WithDetail("field", item.Field).WithCause(err)
		}
		qb = qb.Where(cond)
	}
	return qb, nil
}

// applySimpleFilters adds WHERE clauses for simple (non-executor) datasets.
// Only processes filters that match declared Fields with FilterOnly or dimension kind.
func (c *Compiler) applySimpleFilters(qb squirrel.SelectBuilder, ds *schema.Dataset, filters map[string]any) (squirrel.SelectBuilder, error) {
	for key, value := range filters {
		field := ds.FindField(key)
		if field == nil {
			// Check dataset-level filter defs
			continue
		}

		item := filter.Item{Field: key, Operator: filter.Equal, Value: value}
		if value == nil {
			item.Operator = filter.IsNull
		} else if v := reflect.ValueOf(value); v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
			// An empty dataset parameter means no selection, as for executor dimensions.
			if v.Len() == 0 {
				continue
			}
			item.Operator = filter.InList
		}
		resolved, err := filter.Resolve(item, datasetFilterSpec(field))
		if err != nil {
			return qb, apperror.NewValidation("invalid report parameter").WithDetail("field", key).WithCause(err)
		}
		condition, err := pgfilter.BuildSingleCondition(resolved, "base."+field.Name)
		if err != nil {
			return qb, apperror.NewValidation("invalid report parameter").WithDetail("field", key).WithCause(err)
		}
		qb = qb.Where(condition)
	}
	return qb, nil
}

// normalizeValue converts pgx-specific Go types to JSON-friendly representations.
// Specifically handles:
//   - [16]byte (UUID) → "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" string
//   - time.Time → ISO 8601 string
//   - []byte → hex string
func normalizeValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case [16]byte:
		// UUID: format as standard UUID string
		return formatUUID(val)
	case time.Time:
		return val.Format(time.RFC3339)
	case []byte:
		// Could be a UUID or raw bytes — try UUID first
		if len(val) == 16 {
			var arr [16]byte
			copy(arr[:], val)
			return formatUUID(arr)
		}
		return string(val)
	default:
		return v
	}
}

// formatUUID formats a [16]byte as a standard UUID string.
func formatUUID(b [16]byte) string {
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

// normalizeRawMoney preserves raw integer NUMERIC values without narrowing.
func normalizeRawMoney(value any) (string, error) {
	if numeric, ok := value.(pgtype.Numeric); ok {
		if !numeric.Valid || numeric.NaN || numeric.InfinityModifier != pgtype.Finite || numeric.Int == nil {
			return "", fmt.Errorf("non-finite raw amount")
		}
		amount := decimal.NewFromBigInt(numeric.Int, numeric.Exp)
		if !amount.IsInteger() || amount.IsNegative() {
			return "", fmt.Errorf("invalid raw amount")
		}
		return amount.StringFixed(0), nil
	}
	text, err := normalizeMoney(value)
	if err != nil {
		return "", err
	}
	amount, err := types.NewRawAmountFromString(text)
	return amount.String(), err
}

// normalizeMoney rejects floating point and fractional/out-of-range SQL sums.
func normalizeMoney(value any) (string, error) {
	switch v := value.(type) {
	case int64:
		return strconv.FormatInt(v, 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case pgtype.Numeric:
		if !v.Valid || v.NaN || v.InfinityModifier != pgtype.Finite || v.Int == nil {
			return "", fmt.Errorf("non-finite money")
		}
		integer, err := v.Int64Value()
		if err != nil {
			return "", err
		}
		if !integer.Valid {
			return "", fmt.Errorf("non-finite money")
		}
		return strconv.FormatInt(integer.Int64, 10), nil
	default:
		return "", fmt.Errorf("unexpected monetary SQL type %T", value)
	}
}

func moneyProjectionFields(ds *schema.Dataset, selected []string) ([]string, error) {
	paths := append([]string(nil), selected...)
	seen := make(map[string]bool)
	for _, path := range paths {
		seen[path] = true
	}
	for _, path := range selected {
		field := ds.FindField(path)
		if field == nil || field.Type != schema.TypeMoney {
			continue
		}
		if field.Money == nil {
			return nil, apperror.NewValidation("monetary units are not declared").WithDetail("field", path)
		}
		for _, unitField := range []string{field.Money.CurrencyID, field.Money.CurrencyCode, field.Money.DecimalPlaces} {
			if unitField == "" || ds.FindField(unitField) == nil {
				return nil, apperror.NewValidation("monetary unit source is unavailable").WithDetail("field", path)
			}
			if !seen[unitField] {
				paths = append(paths, unitField)
				seen[unitField] = true
			}
		}
	}
	return paths, nil
}
