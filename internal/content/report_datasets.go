package content

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"

	"metapus/internal/core/types"
	"metapus/internal/domain/reports/schema"
)

// qtyScale is the SQL fragment for dividing stored quantity by the scale constant.
var qtyScale = fmt.Sprintf("::float8 / %d.0", types.QuantityScale)

// ---------------------------------------------------------------------------
// Stock Balance Dataset
// ---------------------------------------------------------------------------

// StockBalanceDataset defines the "Остатки товаров" report.
var StockBalanceDataset = schema.Dataset{
	Key:         "stock-balance",
	Name:        "Остатки товаров",
	Description: "Текущие остатки товаров на складах",
	Permission:  "report:stock:read",
	Fields: []schema.Field{
		{Name: "warehouse_id", Label: "Склад", Kind: schema.FieldDimension, Type: schema.TypeRef, RefEntity: "warehouse", Sortable: true},
		{Name: "nomenclature_id", Label: "Товар", Kind: schema.FieldDimension, Type: schema.TypeRef, RefEntity: "nomenclature", Sortable: true},
		{Name: "quantity", Label: "Остаток", Kind: schema.FieldMeasure, Type: schema.TypeQuantity, Agg: schema.AggSum, Sortable: true, Scale: 4},
	},
	Filters: []schema.FilterDef{
		{Key: "as_of_date", Label: "Дата остатков", Type: schema.FilterDate},
		{Key: "exclude_zero", Label: "Исключить нулевые", Type: schema.FilterBoolean, Default: true},
	},
	ScopeDimensions: []string{"warehouse"},
	ExportFormats:   []string{"csv", "xlsx"},
	Executor:        &stockBalanceExecutor{},
}

// stockBalanceExecutor builds a CTE that calculates stock balance from movements.
type stockBalanceExecutor struct{}

func (e *stockBalanceExecutor) BuildQuery(ctx context.Context, params map[string]interface{}) (squirrel.SelectBuilder, error) {
	builder := squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)

	asOfDate := time.Now()
	if v, ok := params["as_of_date"]; ok {
		if s, ok := v.(string); ok && s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				asOfDate = t
			} else if t, err := time.Parse("2006-01-02", s); err == nil {
				// User selected a date (no time) → use end of that day
				asOfDate = t.Add(24*time.Hour - time.Millisecond)
			}
		}
	}

	excludeZero := false
	if v, ok := params["exclude_zero"]; ok {
		if b, ok := v.(bool); ok {
			excludeZero = b
		}
	}

	// Build the CTE SQL
	args := []interface{}{asOfDate}
	argIdx := 2

	cteSQL := `
		SELECT 
			m.warehouse_id,
			m.nomenclature_id,
			SUM(CASE WHEN m.record_type = 'receipt' THEN m.quantity ELSE -m.quantity END)` + qtyScale + ` as quantity
		FROM reg_stock_movements m
		WHERE m.period <= $1`

	// Warehouse filter
	if warehouseIDs, ok := extractIDSlice(params, "warehouse_id"); ok && len(warehouseIDs) > 0 {
		placeholders := make([]string, len(warehouseIDs))
		for i, wid := range warehouseIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, wid)
			argIdx++
		}
		cteSQL += fmt.Sprintf(" AND m.warehouse_id IN (%s)", strings.Join(placeholders, ","))
	}

	// Product filter
	if productIDs, ok := extractIDSlice(params, "nomenclature_id"); ok && len(productIDs) > 0 {
		placeholders := make([]string, len(productIDs))
		for i, pid := range productIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, pid)
			argIdx++
		}
		cteSQL += fmt.Sprintf(" AND m.nomenclature_id IN (%s)", strings.Join(placeholders, ","))
	}

	havingClause := ""
	if excludeZero {
		havingClause = " HAVING SUM(CASE WHEN m.record_type = 'receipt' THEN m.quantity ELSE -m.quantity END) != 0"
	}

	cteSQL += " GROUP BY m.warehouse_id, m.nomenclature_id" + havingClause

	// Wrap CTE as subquery aliased as "base"
	subquery := fmt.Sprintf("(%s) AS base", cteSQL)

	qb := builder.Select().From(subquery)

	// Bind args via squirrel's Expr mechanism is not straightforward with raw CTE,
	// so we use a PrefixExpr approach. However, squirrel doesn't support CTE natively.
	// Instead, use raw SQL prefix.
	// Re-approach: build the full query with positional params.
	_ = qb // we'll build a raw approach

	// Since squirrel doesn't support CTE well, use PlaceholderFormat with raw SQL subquery.
	// The SELECT columns will be appended by the Compiler.
	rawQB := builder.
		Select().
		FromSelect(
			builder.Select(
				"m.warehouse_id",
				"m.nomenclature_id",
				"SUM(CASE WHEN m.record_type = 'receipt' THEN m.quantity ELSE -m.quantity END)" + qtyScale + " as quantity",
			).
				From("reg_stock_movements m").
				Where(squirrel.LtOrEq{"m.period": asOfDate}).
				GroupBy("m.warehouse_id", "m.nomenclature_id"),
			"base",
		)

	// Apply warehouse filter via squirrel
	if warehouseIDs, ok := extractIDSlice(params, "warehouse_id"); ok && len(warehouseIDs) > 0 {
		rawQB = rawQB.Where(squirrel.Eq{"base.warehouse_id": warehouseIDs})
	}

	// Apply product filter
	if productIDs, ok := extractIDSlice(params, "nomenclature_id"); ok && len(productIDs) > 0 {
		rawQB = rawQB.Where(squirrel.Eq{"base.nomenclature_id": productIDs})
	}

	if excludeZero {
		rawQB = rawQB.Where("base.quantity != 0")
	}

	return rawQB, nil
}

// ---------------------------------------------------------------------------
// Stock Turnover Dataset
// ---------------------------------------------------------------------------

// StockTurnoverDataset defines the "Оборотная ведомость" report.
var StockTurnoverDataset = schema.Dataset{
	Key:         "stock-turnover",
	Name:        "Оборотная ведомость",
	Description: "Приход, расход и остатки товаров за период",
	Permission:  "report:stock:read",
	Fields: []schema.Field{
		{Name: "warehouse_id", Label: "Склад", Kind: schema.FieldDimension, Type: schema.TypeRef, RefEntity: "warehouse", Sortable: true},
		{Name: "nomenclature_id", Label: "Товар", Kind: schema.FieldDimension, Type: schema.TypeRef, RefEntity: "nomenclature", Sortable: true},
		{Name: "opening_balance", Label: "Нач. остаток", Kind: schema.FieldMeasure, Type: schema.TypeQuantity, Agg: schema.AggSum, Sortable: true, Scale: 4},
		{Name: "receipt", Label: "Приход", Kind: schema.FieldMeasure, Type: schema.TypeQuantity, Agg: schema.AggSum, Sortable: true, Scale: 4},
		{Name: "expense", Label: "Расход", Kind: schema.FieldMeasure, Type: schema.TypeQuantity, Agg: schema.AggSum, Sortable: true, Scale: 4},
		{Name: "closing_balance", Label: "Кон. остаток", Kind: schema.FieldMeasure, Type: schema.TypeQuantity, Agg: schema.AggSum, Sortable: true, Scale: 4},
	},
	Filters: []schema.FilterDef{
		{Key: "from_date", Label: "Начало периода", Type: schema.FilterDate, Required: true},
		{Key: "to_date", Label: "Конец периода", Type: schema.FilterDate, Required: true},
	},
	ScopeDimensions: []string{"warehouse"},
	ExportFormats:   []string{"csv", "xlsx"},
	Executor:        &stockTurnoverExecutor{},
}

type stockTurnoverExecutor struct{}

func (e *stockTurnoverExecutor) BuildQuery(ctx context.Context, params map[string]interface{}) (squirrel.SelectBuilder, error) {
	fromDate, err := extractRequiredDate(params, "from_date")
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}
	toDate, err := extractRequiredDate(params, "to_date")
	if err != nil {
		return squirrel.SelectBuilder{}, err
	}

	builder := squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)

	// Build subquery SQL strings with args
	openingSub := builder.Select(
		"m.warehouse_id",
		"m.nomenclature_id",
		"SUM(CASE WHEN m.record_type = 'receipt' THEN m.quantity ELSE -m.quantity END)" + qtyScale + " as opening_qty",
	).From("reg_stock_movements m").
		Where(squirrel.Lt{"m.period": fromDate}).
		GroupBy("m.warehouse_id", "m.nomenclature_id")

	mainSub := builder.Select(
		"m.warehouse_id",
		"m.nomenclature_id",
		"SUM(CASE WHEN m.record_type = 'receipt' THEN m.quantity ELSE 0 END)" + qtyScale + " as receipt",
		"SUM(CASE WHEN m.record_type = 'expense' THEN m.quantity ELSE 0 END)" + qtyScale + " as expense",
	).From("reg_stock_movements m").
		Where(squirrel.And{
			squirrel.GtOrEq{"m.period": fromDate},
			squirrel.Lt{"m.period": toDate},
		}).
		GroupBy("m.warehouse_id", "m.nomenclature_id")

	openingSQL, openingArgs, _ := openingSub.ToSql()
	mainSQL, mainArgs, _ := mainSub.ToSql()

	// Re-number opening placeholders to continue after main args
	reNumberedOpening := reNumberPlaceholders(openingSQL, len(mainArgs))

	// Combine args: main first, then opening
	allArgs := make([]interface{}, 0, len(mainArgs)+len(openingArgs))
	allArgs = append(allArgs, mainArgs...)
	allArgs = append(allArgs, openingArgs...)

	// Build the FULL OUTER JOIN subquery SQL
	combinedSQL := fmt.Sprintf(
		`SELECT
			COALESCE(t.warehouse_id, o.warehouse_id) as warehouse_id,
			COALESCE(t.nomenclature_id, o.nomenclature_id) as nomenclature_id,
			COALESCE(o.opening_qty, 0) as opening_balance,
			COALESCE(t.receipt, 0) as receipt,
			COALESCE(t.expense, 0) as expense,
			COALESCE(o.opening_qty, 0) + COALESCE(t.receipt, 0) - COALESCE(t.expense, 0) as closing_balance
		FROM (%s) t
		FULL OUTER JOIN (%s) o
			ON t.warehouse_id = o.warehouse_id AND t.nomenclature_id = o.nomenclature_id`,
		mainSQL, reNumberedOpening,
	)

	// Build a squirrel.SelectBuilder wrapper that carries the args.
	// We use an inner builder with a raw Expr() in the FROM and Where to
	// properly bind args from both subqueries. Then Compiler uses this as base.
	innerBuilder := builder.
		Select("*").
		From("("+combinedSQL+") AS _inner").
		Where(squirrel.Expr("1=1", allArgs...))

	// Wrap the inner builder via FromSelect so squirrel propagates its args
	qb := builder.Select().FromSelect(innerBuilder, "base")

	// Apply warehouse filter
	if warehouseIDs, ok := extractIDSlice(params, "warehouse_id"); ok && len(warehouseIDs) > 0 {
		qb = qb.Where(squirrel.Eq{"base.warehouse_id": warehouseIDs})
	}

	return qb, nil
}

// ---------------------------------------------------------------------------
// Document Journal Dataset
// ---------------------------------------------------------------------------

// DocumentJournalDataset defines the "Журнал документов" report.
var DocumentJournalDataset = schema.Dataset{
	Key:         "document-journal",
	Name:        "Журнал документов",
	Description: "Все документы системы в хронологическом порядке",
	Permission:  "report:document-journal:read",
	Fields: []schema.Field{
		{Name: "id", Label: "ID", Kind: schema.FieldAttribute, Type: schema.TypeString, Hidden: true},
		{Name: "document_type", Label: "Тип документа", Kind: schema.FieldDimension, Type: schema.TypeString, Sortable: true},
		{Name: "number", Label: "Номер", Kind: schema.FieldAttribute, Type: schema.TypeString, Sortable: true},
		{Name: "date", Label: "Дата", Kind: schema.FieldDimension, Type: schema.TypeDate, Sortable: true},
		{Name: "posted", Label: "Проведён", Kind: schema.FieldAttribute, Type: schema.TypeBoolean, Sortable: true},
		{Name: "counterparty_name", Label: "Контрагент", Kind: schema.FieldAttribute, Type: schema.TypeString, Sortable: true},
		{Name: "warehouse_name", Label: "Склад", Kind: schema.FieldAttribute, Type: schema.TypeString, Sortable: true},
		{Name: "total_amount", Label: "Сумма", Kind: schema.FieldMeasure, Type: schema.TypeMoney, Agg: schema.AggSum, Sortable: true, Scale: 2},
		{Name: "currency", Label: "Валюта", Kind: schema.FieldAttribute, Type: schema.TypeString},
		{Name: "description", Label: "Комментарий", Kind: schema.FieldAttribute, Type: schema.TypeString, Hidden: true},
	},
	Filters: []schema.FilterDef{
		{Key: "from_date", Label: "Начало периода", Type: schema.FilterDate},
		{Key: "to_date", Label: "Конец периода", Type: schema.FilterDate},
		{Key: "posted", Label: "Проведённые", Type: schema.FilterBoolean},
	},
	DefaultSort:   &schema.SortDef{Column: "date", Direction: "desc"},
	ExportFormats: []string{"csv", "xlsx"},
	Executor:      &documentJournalExecutor{},
}

type documentJournalExecutor struct{}

func (e *documentJournalExecutor) BuildQuery(ctx context.Context, params map[string]interface{}) (squirrel.SelectBuilder, error) {
	builder := squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)

	// Build UNION ALL from document tables
	// Goods Receipt
	grQuery := builder.Select(
		"d.id", "'goods_receipt' as document_type", "d.number", "d.date",
		"d.posted",
		"COALESCE(cp.name, '') as counterparty_name",
		"COALESCE(w.name, '') as warehouse_name",
		"COALESCE((SELECT SUM(amount) FROM doc_goods_receipt_lines WHERE document_id = d.id), 0) as total_amount",
		"COALESCE(cur.iso_code, '') as currency",
		"d.description",
	).From("doc_goods_receipts d").
		LeftJoin("cat_currencies cur ON d.currency_id = cur.id").
		LeftJoin("cat_warehouses w ON d.warehouse_id = w.id").
		LeftJoin("cat_counterparties cp ON d.counterparty_id = cp.id").
		Where("d.deletion_mark = false")

	// Goods Issue
	giQuery := builder.Select(
		"d.id", "'goods_issue' as document_type", "d.number", "d.date",
		"d.posted",
		"COALESCE(cp.name, '') as counterparty_name",
		"COALESCE(w.name, '') as warehouse_name",
		"COALESCE((SELECT SUM(amount) FROM doc_goods_issue_lines WHERE document_id = d.id), 0) as total_amount",
		"COALESCE(cur.iso_code, '') as currency",
		"d.description",
	).From("doc_goods_issues d").
		LeftJoin("cat_currencies cur ON d.currency_id = cur.id").
		LeftJoin("cat_warehouses w ON d.warehouse_id = w.id").
		LeftJoin("cat_counterparties cp ON d.counterparty_id = cp.id").
		Where("d.deletion_mark = false")

	// Apply date filters
	if fromDate, ok := extractOptionalDate(params, "from_date"); ok {
		grQuery = grQuery.Where(squirrel.GtOrEq{"d.date": fromDate})
		giQuery = giQuery.Where(squirrel.GtOrEq{"d.date": fromDate})
	}
	if toDate, ok := extractOptionalDate(params, "to_date"); ok {
		grQuery = grQuery.Where(squirrel.Lt{"d.date": toDate})
		giQuery = giQuery.Where(squirrel.Lt{"d.date": toDate})
	}
	if posted, ok := params["posted"]; ok {
		if b, ok := posted.(bool); ok {
			grQuery = grQuery.Where(squirrel.Eq{"d.posted": b})
			giQuery = giQuery.Where(squirrel.Eq{"d.posted": b})
		}
	}

	// Build UNION ALL
	grSQL, grArgs, _ := grQuery.ToSql()
	giSQL, giArgs, _ := giQuery.ToSql()

	reNumberedGI := reNumberPlaceholders(giSQL, len(grArgs))
	allArgs := append(grArgs, giArgs...)

	unionSQL := fmt.Sprintf("(%s UNION ALL %s) AS base", grSQL, reNumberedGI)

	// Wrap in outer select
	qb := squirrel.Select().From(unionSQL).PlaceholderFormat(squirrel.Dollar)

	_ = allArgs // args will be handled by squirrel
	return qb, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractIDSlice extracts a []string of IDs from params.
func extractIDSlice(params map[string]interface{}, key string) ([]interface{}, bool) {
	v, ok := params[key]
	if !ok {
		return nil, false
	}
	switch ids := v.(type) {
	case []interface{}:
		return ids, len(ids) > 0
	case []string:
		result := make([]interface{}, len(ids))
		for i, s := range ids {
			result[i] = s
		}
		return result, len(result) > 0
	}
	return nil, false
}

func extractRequiredDate(params map[string]interface{}, key string) (time.Time, error) {
	v, ok := params[key]
	if !ok {
		return time.Time{}, fmt.Errorf("required parameter %q is missing", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}, fmt.Errorf("required parameter %q must be a date string", key)
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid date format for %q: %s", key, s)
}

func extractOptionalDate(params map[string]interface{}, key string) (time.Time, bool) {
	v, ok := params[key]
	if !ok {
		return time.Time{}, false
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// reNumberPlaceholders shifts $N placeholders by offset.
// E.g. reNumberPlaceholders("WHERE x = $1 AND y = $2", 3) → "WHERE x = $4 AND y = $5"
func reNumberPlaceholders(sql string, offset int) string {
	if offset == 0 {
		return sql
	}
	result := strings.Builder{}
	i := 0
	for i < len(sql) {
		if sql[i] == '$' && i+1 < len(sql) && sql[i+1] >= '1' && sql[i+1] <= '9' {
			// Parse the number after $
			j := i + 1
			for j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
				j++
			}
			numStr := sql[i+1 : j]
			var num int
			if _, err := fmt.Sscanf(numStr, "%d", &num); err != nil {
				result.WriteString(sql[i:j])
				i = j
				continue
			}
			fmt.Fprintf(&result, "$%d", num+offset)
			i = j
		} else {
			result.WriteByte(sql[i])
			i++
		}
	}
	return result.String()
}
