---
name: optimizing-analytical-reporting
description: Use when creating analytical queries, reports, or data exporters. Enforces efficient use of accumulation registers and high-performance SQL patterns.
---

# Optimizing Analytical Reporting in Metapus

This skill ensures that analytical reports are performant, follow the account model, and scale effectively.

## Goal
To provide a structured approach to querying transactional data (registers) for reporting purposes, similar to 1C's SKD.

## Core Principles
1. **QUERY REGISTERS, NOT DOCUMENTS**: Always get data from accumulation or information registers for analytical reports.
2. **SLICE-LAST PATTERN**: Efficiently retrieve the latest state for information registers.
3. **TURN-OVER CALCULATION**: Use range-based aggregations on movement tables.
4. **NO N+1 QUERIES**: Batch data retrieval and use optimized joins.

## Instructions

### 1. Slice-Last Pattern
For periodic information registers, use `DISTINCT ON` for performance:
```sql
SELECT DISTINCT ON (currency_id)
    currency_id, rate, period
FROM reg_info_currency_rates
WHERE period <= $1 -- Target Date
ORDER BY currency_id, period DESC;
```

### 2. Balance and Turnovers
Use the "hot cache" `reg_*_balances` for current state and `reg_*_movements` for historical turnover analysis.

### 3. Reporting DTOs
Define reporting DTOs that align with the frontend's needs (e.g., grouped by dimensions, calculated totals).

## Advanced Pattern: Dynamic Aggregation Engine (SKD-like)

For maximum flexibility and performance, implement a generic reporting engine based on metadata.

### 1. Report Metadata (`ReportDef`)
Describe reports in Go structs as the source of truth for the engine.
- **Dimensions**: Fields for grouping (e.g., Warehouse, Product).
- **Measures**: Fields for aggregation (e.g., Quantity, Amount) with specific formulas.

### 2. Universal Reporting Repository
Implement a `GenericReportRepo` that uses `squirrel` to dynamically build SQL based on a `ReportRequest` (Select, GroupBy, Filters).

### 3. Leveraging SQL Views
Instead of complex JOINs in Go code, create PostgreSQL Views (`rpt_*`) that flatten the data for the reporting engine. This moves the logic to the DB layer while keeping the Go code generic.

### 4. Reusing Filters
Use the project's standard `filter` package to apply dynamic WHERE clauses to the report queries, ensuring consistency with list views.

## Examples

### Example: Dynamic Stock Balance Query
```go
// Using squirrel to build the dynamic aggregation
q := squirrel.Select().From("rpt_stock_balance")
for _, dim := range req.GroupBy {
    q = q.Column(dim).GroupBy(dim)
}
for _, measure := range activeMeasures {
    q = q.Column(fmt.Sprintf("%s(%s) as %s", measure.Agg, measure.Expr, measure.Name))
}
```

## Constraints
- **Do NOT** perform heavy aggregations on document tables.
- **Always** validate requested fields against the "white-list" in metadata to prevent SQL injection.
- **Always** use indexes appropriately for filter dimensions (e.g., WarehouseID, ProductID).
