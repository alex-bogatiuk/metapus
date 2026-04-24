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
	"strings"
	"time"

	"github.com/Masterminds/squirrel"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/filter"
	"metapus/internal/domain/reports/schema"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/metadata"
)

// MaxJoinDepth is the maximum depth for reference field resolution.
// Paths like "product_id.brand_id.country_id.name" have depth 3.
const MaxJoinDepth = 3

// QueryRequest is the JSON body sent by the frontend to execute a report.
type QueryRequest struct {
	// Dataset is the dataset key, e.g. "stock-balance".
	Dataset string `json:"dataset"`

	// Select lists field paths to include in the result.
	// Simple fields: "quantity", "warehouse_id"
	// Dereferenced: "warehouse_id.name", "product_id.brand_id.name"
	// If empty, all non-hidden fields are selected.
	Select []string `json:"select,omitempty"`

	// GroupBy lists field paths to group by.
	GroupBy []string `json:"groupBy,omitempty"`

	// OrderBy is the field path for sorting.
	OrderBy string `json:"orderBy,omitempty"`

	// OrderDir is "asc" or "desc". Defaults to "asc".
	OrderDir string `json:"orderDir,omitempty"`

	// Filters contains dataset-level parameter values (used by Executors to shape CTEs).
	// E.g. {"warehouse_id": ["uuid1"], "as_of_date": "2025-01-01"}
	Filters map[string]interface{} `json:"filters,omitempty"`

	// AdvancedFilters are typed filter conditions from FilterSidebar.
	// Each item's Field can be a dot-path (e.g. "product_id.brand_id.name"),
	// resolved via the path resolver with automatic JOIN generation.
	// All conditions are compiled into SQL WHERE clauses (push-down).
	AdvancedFilters []filter.Item `json:"advancedFilters,omitempty"`

	// Limit caps the number of rows returned.
	Limit int `json:"limit,omitempty"`

	// Offset for pagination.
	Offset int `json:"offset,omitempty"`

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
	Items      []map[string]interface{} `json:"items"`
	TotalItems int                      `json:"totalItems"`
}

// Compiler is the core Query Engine.
// Thread-safe after construction — all mutable state is per-request.
type Compiler struct {
	registry *metadata.Registry
	datasets map[string]*schema.Dataset
	builder  squirrel.StatementBuilderType
}

// NewCompiler creates a Compiler with the given metadata registry and datasets.
func NewCompiler(reg *metadata.Registry, datasets []*schema.Dataset) *Compiler {
	dsMap := make(map[string]*schema.Dataset, len(datasets))
	for _, ds := range datasets {
		dsMap[ds.Key] = ds
	}
	return &Compiler{
		registry: reg,
		datasets: dsMap,
		builder:  squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
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

// Execute compiles and runs a QueryRequest, returning the result.
func (c *Compiler) Execute(ctx context.Context, req QueryRequest) (*QueryResult, error) {
	ds, ok := c.datasets[req.Dataset]
	if !ok {
		return nil, apperror.NewInternal(fmt.Errorf("unknown dataset: %q", req.Dataset))
	}

	// 1. Determine selected fields (default to all non-hidden)
	selectPaths := req.Select
	if len(selectPaths) == 0 {
		selectPaths = ds.DefaultSelectedFields()
	}

	// 2. Resolve all field paths → collect JoinSteps and SELECT expressions
	resolver := newResolver(c.registry, ds, MaxJoinDepth)

	selectExprs := make([]string, 0, len(selectPaths))
	for _, path := range selectPaths {
		expr, err := resolver.Resolve(path)
		if err != nil {
			return nil, apperror.NewInternal(fmt.Errorf("invalid field path %q: %v", path, err))
		}
		selectExprs = append(selectExprs, expr)
	}

	// 3. Build the base query
	var qb squirrel.SelectBuilder

	if ds.Executor != nil {
		// Complex dataset — delegate to custom executor
		var err error
		qb, err = ds.Executor.BuildQuery(ctx, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("dataset executor %q: %v", ds.Key, err)
		}
	} else {
		// Simple dataset — SELECT from BaseTable
		qb = c.builder.Select().From(ds.BaseTable + " AS base")
		qb = c.applySimpleFilters(qb, ds, req.Filters)
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
			return nil, err
		}
	}

	// 5c. Append LEFT JOINs from resolver (includes JOINs from select + filters)
	for _, join := range resolver.Joins() {
		joinClause := fmt.Sprintf("%s AS %s ON %s.%s = %s.id",
			join.Table, join.Alias, join.ParentAlias, join.JoinKey, join.Alias)
		qb = qb.LeftJoin(joinClause)
	}

	// 6. GROUP BY
	if len(req.GroupBy) > 0 {
		groupExprs := make([]string, 0, len(req.GroupBy))
		for _, gPath := range req.GroupBy {
			expr, err := resolver.ResolveForGroupBy(gPath)
			if err != nil {
				return nil, apperror.NewInternal(fmt.Errorf("invalid groupBy path %q: %v", gPath, err))
			}
			groupExprs = append(groupExprs, expr)
		}
		qb = qb.GroupBy(groupExprs...)
	}

	// 7. ORDER BY
	// If ExportGroupBy is present, these keys MUST be the primary sort keys (ASC)
	// so that streaming control breaks can detect group changes.
	if len(req.ExportGroupBy) > 0 {
		for _, gPath := range req.ExportGroupBy {
			// Frontend sends display keys with __, resolver expects dot notation
			sqlPath := strings.ReplaceAll(gPath, "__", ".")
			orderExpr, err := resolver.ResolveForOrderBy(sqlPath)
			if err != nil {
				return nil, apperror.NewInternal(fmt.Errorf("invalid exportGroupBy path %q: %v", gPath, err))
			}
			// Groups are always sorted ascending to keep hierarchy stable
			qb = qb.OrderBy(orderExpr + " ASC")
		}
	}

	if req.OrderBy != "" {
		orderExpr, err := resolver.ResolveForOrderBy(req.OrderBy)
		if err != nil {
			return nil, apperror.NewInternal(fmt.Errorf("invalid orderBy path %q: %v", req.OrderBy, err))
		}
		dir := "ASC"
		if strings.EqualFold(req.OrderDir, "desc") {
			dir = "DESC"
		}
		qb = qb.OrderBy(orderExpr + " " + dir)
	} else if ds.DefaultSort != nil {
		dir := "ASC"
		if strings.EqualFold(ds.DefaultSort.Direction, "desc") {
			dir = "DESC"
		}
		qb = qb.OrderBy("base." + ds.DefaultSort.Column + " " + dir)
	}

	// 8. LIMIT / OFFSET
	if req.Limit > 0 {
		qb = qb.Limit(uint64(req.Limit))
	}
	if req.Offset > 0 {
		qb = qb.Offset(uint64(req.Offset))
	}

	// 9. Execute
	query, args, err := qb.ToSql()
	if err != nil {
		return nil, apperror.NewValidation(fmt.Sprintf("build SQL: %v", err))
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, query, args...)
	if err != nil {
		return nil, apperror.NewValidation(fmt.Sprintf("execute query: %v", err))
	}
	defer rows.Close()

	// Manual row scanning: converts pgx types (UUID [16]byte, etc.) to JSON-friendly values.
	items := make([]map[string]interface{}, 0)
	fieldDescs := rows.FieldDescriptions()
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, apperror.NewValidation(fmt.Sprintf("scan row values: %v", err))
		}

		row := make(map[string]interface{}, len(fieldDescs))
		for i, fd := range fieldDescs {
			key := string(fd.Name)
			val := values[i]
			row[key] = normalizeValue(val)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.NewValidation(fmt.Sprintf("rows iteration: %v", err))
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
//   "product_id.brand_id.name" → LEFT JOIN + WHERE j2.name ILIKE '%...'
//
// All filtering happens at the SQL level (push-down) — zero post-fetch filtering.
func (c *Compiler) applyAdvancedFilters(
	qb squirrel.SelectBuilder,
	r *resolver,
	items []filter.Item,
) (squirrel.SelectBuilder, error) {
	if err := filter.ValidateItems(items); err != nil {
		return qb, apperror.NewValidation(fmt.Sprintf("invalid filter: %v", err))
	}

	for _, item := range items {
		// Resolve the field path → SQL expression + register JOINs
		sqlExpr, err := r.ResolveForWhere(item.Field)
		if err != nil {
			return qb, apperror.NewValidation(fmt.Sprintf("filter field %q: %v", item.Field, err))
		}

		// Date fields need DATE() wrapper for date-only comparison
		if item.FieldType == "date" {
			sqlExpr = "DATE(" + sqlExpr + ")"
		}

		// Delegate operator→SQL to the shared filter package
		cond, err := filter.BuildSingleCondition(item, sqlExpr)
		if err != nil {
			return qb, apperror.NewValidation(fmt.Sprintf("filter %q: %v", item.Field, err))
		}
		qb = qb.Where(cond)
	}
	return qb, nil
}

// applySimpleFilters adds WHERE clauses for simple (non-executor) datasets.
// Only processes filters that match declared Fields with FilterOnly or dimension kind.
func (c *Compiler) applySimpleFilters(qb squirrel.SelectBuilder, ds *schema.Dataset, filters map[string]interface{}) squirrel.SelectBuilder {
	for key, value := range filters {
		field := ds.FindField(key)
		if field == nil {
			// Check dataset-level filter defs
			continue
		}

		col := "base." + field.Name

		// Handle array values (IN clause)
		switch v := value.(type) {
		case []interface{}:
			if len(v) > 0 {
				qb = qb.Where(squirrel.Eq{col: v})
			}
		default:
			qb = qb.Where(squirrel.Eq{col: value})
		}
	}
	return qb
}

// normalizeValue converts pgx-specific Go types to JSON-friendly representations.
// Specifically handles:
//   - [16]byte (UUID) → "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" string
//   - time.Time → ISO 8601 string
//   - []byte → hex string
func normalizeValue(v interface{}) interface{} {
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
