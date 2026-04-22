package schema

import (
	"context"

	"github.com/Masterminds/squirrel"
)

// DatasetExecutor is an optional interface for datasets with complex SQL logic.
//
// Simple datasets (flat SELECT from a single table) don't need an Executor —
// the Query Compiler generates SQL automatically from Dataset.BaseTable + Fields.
//
// Implement DatasetExecutor when the dataset requires:
//   - CTE (Common Table Expression), e.g. stock balance calculation
//   - Parametric subqueries, e.g. opening balance depends on "as_of_date"
//   - UNION ALL across multiple tables, e.g. document journal
//   - Custom aggregation logic that can't be expressed declaratively
//
// The Query Compiler calls BuildQuery() to get the "base" query, then appends
// dynamic LEFT JOINs for reference field dereferencing (auto-discovery).
//
// Example (Stock Balance):
//
//	func (e *StockBalanceExecutor) BuildQuery(ctx context.Context, params map[string]interface{}) (squirrel.SelectBuilder, error) {
//	    asOfDate := extractDate(params, "as_of_date", time.Now())
//	    // Build CTE: SUM movements WHERE period <= asOfDate, GROUP BY warehouse+product
//	    return builder, nil
//	}
type DatasetExecutor interface {
	// BuildQuery constructs the base SQL query for the dataset.
	//
	// params contains filter values from the frontend QueryRequest.Filters,
	// already validated by the Compiler against Dataset.Filters/Fields.
	//
	// Returns a squirrel.SelectBuilder with:
	//   - FROM clause (table, CTE, or subquery)
	//   - WHERE conditions from params
	//   - GROUP BY if the dataset is pre-aggregated
	//
	// The Compiler will add to this builder:
	//   - Additional SELECT columns for dereferenced reference fields
	//   - LEFT JOINs for reference resolution (auto-discovery)
	//   - ORDER BY, LIMIT, OFFSET from QueryRequest
	//
	// The builder MUST use "base" as the alias for the main table/subquery
	// so the Compiler can reference columns as "base.column_name".
	BuildQuery(ctx context.Context, params map[string]interface{}) (squirrel.SelectBuilder, error)
}
