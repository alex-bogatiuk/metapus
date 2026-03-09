// Package report_repo provides PostgreSQL implementations for report repositories.
// In Database-per-Tenant architecture, TxManager is obtained from context.
package report_repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/domain/reports"
	"metapus/internal/infrastructure/storage/postgres"
)

// ReportRepo implements reports.Repository.
// In Database-per-Tenant architecture, TxManager is obtained from context.
type ReportRepo struct {
	builder squirrel.StatementBuilderType
}

// NewReportRepo creates a new report repository.
func NewReportRepo() *ReportRepo {
	return &ReportRepo{
		builder: squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}
}

// getTxManager retrieves TxManager from context.
func (r *ReportRepo) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// GetStockBalanceReport generates stock balance report with product/warehouse details.
func (r *ReportRepo) GetStockBalanceReport(ctx context.Context, filter reports.StockBalanceReportFilter) (*reports.StockBalanceReport, error) {
	asOfDate := time.Now()
	if filter.AsOfDate != nil {
		asOfDate = *filter.AsOfDate
	}

	// Build query with JOINs to get product and warehouse names
	query := `
		WITH balance_data AS (
			SELECT 
				m.warehouse_id,
				m.product_id,
				SUM(CASE WHEN m.record_type = 'receipt' THEN m.quantity ELSE -m.quantity END) as quantity_scaled
			FROM reg_stock_movements m
			WHERE m.period <= $1
	`
	args := []any{asOfDate}
	argIndex := 2

	if len(filter.WarehouseIDs) > 0 {
		placeholders := make([]string, len(filter.WarehouseIDs))
		for i, whID := range filter.WarehouseIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, whID)
			argIndex++
		}
		query += fmt.Sprintf(" AND m.warehouse_id IN (%s)", strings.Join(placeholders, ","))
	}

	if len(filter.ProductIDs) > 0 {
		placeholders := make([]string, len(filter.ProductIDs))
		for i, pID := range filter.ProductIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, pID)
			argIndex++
		}
		query += fmt.Sprintf(" AND m.product_id IN (%s)", strings.Join(placeholders, ","))
	}

	havingClause := "HAVING SUM(CASE WHEN m.record_type = 'receipt' THEN m.quantity ELSE -m.quantity END) != 0"
	if !filter.ExcludeZero {
		havingClause = ""
	}

	query += fmt.Sprintf(`
			GROUP BY m.warehouse_id, m.product_id
			%s
		)
		SELECT 
			bd.warehouse_id,
			w.name as warehouse_name,
			bd.product_id,
			p.name as product_name,
			COALESCE(p.article, '') as product_sku,
			COALESCE(u.name, '') as unit_name,
			bd.quantity_scaled::float8 / 10000.0 as quantity,
			0 as total_cost
		FROM balance_data bd
		JOIN cat_warehouses w ON bd.warehouse_id = w.id
		JOIN cat_nomenclature p ON bd.product_id = p.id
		LEFT JOIN cat_units u ON p.base_unit_id = u.id
		ORDER BY w.name, p.name
	`, havingClause)

	var items []reports.StockBalanceReportItem
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &items, query, args...); err != nil {
		return nil, fmt.Errorf("stock balance report: %w", err)
	}

	// Calculate totals
	var totalQuantity float64
	for _, item := range items {
		totalQuantity += item.Quantity
	}

	return &reports.StockBalanceReport{
		AsOfDate:      asOfDate,
		Items:         items,
		TotalItems:    len(items),
		TotalQuantity: totalQuantity,
	}, nil
}

// GetStockTurnoverReport generates stock turnover report.
func (r *ReportRepo) GetStockTurnoverReport(ctx context.Context, filter reports.StockTurnoverReportFilter) (*reports.StockTurnoverReport, error) {
	if filter.FromDate.IsZero() || filter.ToDate.IsZero() {
		return nil, fmt.Errorf("from_date and to_date are required")
	}

	args := []any{filter.FromDate}
	argIndex := 2

	// Opening balance query
	openingQuery := `
		SELECT 
			m.warehouse_id,
			m.product_id,
			SUM(CASE WHEN m.record_type = 'receipt' THEN m.quantity ELSE -m.quantity END) as quantity_scaled
		FROM reg_stock_movements m
		WHERE m.period < $1
	`

	if len(filter.WarehouseIDs) > 0 {
		placeholders := make([]string, len(filter.WarehouseIDs))
		for i, whID := range filter.WarehouseIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, whID)
			argIndex++
		}
		openingQuery += fmt.Sprintf(" AND m.warehouse_id IN (%s)", strings.Join(placeholders, ","))
	}

	openingQuery += " GROUP BY m.warehouse_id, m.product_id"

	// Turnover query
	turnoverQuery := fmt.Sprintf(`
		SELECT 
			m.warehouse_id,
			w.name as warehouse_name,
			m.product_id,
			p.name as product_name,
			COALESCE(p.article, '') as product_sku,
			COALESCE(u.name, '') as unit_name,
			COALESCE(opening.quantity_scaled, 0)::float8 / 10000.0 as opening_balance,
			SUM(CASE WHEN m.record_type = 'receipt' THEN m.quantity ELSE 0 END)::float8 / 10000.0 as receipt,
			SUM(CASE WHEN m.record_type = 'expense' THEN m.quantity ELSE 0 END)::float8 / 10000.0 as expense,
			(COALESCE(opening.quantity_scaled, 0) + 
				SUM(CASE WHEN m.record_type = 'receipt' THEN m.quantity ELSE -m.quantity END))::float8 / 10000.0 as closing_balance
		FROM reg_stock_movements m
		JOIN cat_warehouses w ON m.warehouse_id = w.id
		JOIN cat_nomenclature p ON m.product_id = p.id
		LEFT JOIN cat_units u ON p.base_unit_id = u.id
		LEFT JOIN (%s) opening 
			ON m.warehouse_id = opening.warehouse_id AND m.product_id = opening.product_id
		WHERE m.period >= $%d AND m.period < $%d
	`, openingQuery, argIndex, argIndex+1)

	args = append(args, filter.FromDate, filter.ToDate)
	argIndex += 2

	if len(filter.WarehouseIDs) > 0 {
		placeholders := make([]string, len(filter.WarehouseIDs))
		for i, whID := range filter.WarehouseIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, whID)
			argIndex++
		}
		turnoverQuery += fmt.Sprintf(" AND m.warehouse_id IN (%s)", strings.Join(placeholders, ","))
	}

	turnoverQuery += `
		GROUP BY m.warehouse_id, w.name, m.product_id, p.name, p.article, u.name, opening.quantity
		ORDER BY w.name, p.name
	`

	var items []reports.StockTurnoverReportItem
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &items, turnoverQuery, args...); err != nil {
		return nil, fmt.Errorf("stock turnover report: %w", err)
	}

	// Calculate totals
	var totalOpening, totalReceipt, totalExpense, totalClosing float64
	for _, item := range items {
		totalOpening += item.OpeningBalance
		totalReceipt += item.Receipt
		totalExpense += item.Expense
		totalClosing += item.ClosingBalance
	}

	return &reports.StockTurnoverReport{
		FromDate:     filter.FromDate,
		ToDate:       filter.ToDate,
		Items:        items,
		TotalItems:   len(items),
		TotalOpening: totalOpening,
		TotalReceipt: totalReceipt,
		TotalExpense: totalExpense,
		TotalClosing: totalClosing,
	}, nil
}

// GetDocumentJournal retrieves documents for journal view.
func (r *ReportRepo) GetDocumentJournal(ctx context.Context, filter reports.DocumentJournalFilter) (*reports.DocumentJournal, error) {
	// Build union query for different document types
	docTypes := filter.DocumentTypes
	if len(docTypes) == 0 {
		docTypes = []string{"goods_receipt", "goods_issue"}
	}

	var unions []string
	var args []any
	argIndex := 1

	for _, docType := range docTypes {
		switch docType {
		case "goods_receipt":
			q := `
				SELECT 
					id, 'goods_receipt' as document_type, number, date,
					posted, organization_id,
					NULL::uuid as counterparty_id, '' as counterparty_name,
					warehouse_id, '' as warehouse_name,
					0.0 as total_quantity,
					COALESCE((SELECT SUM(amount) FROM doc_goods_receipt_lines WHERE document_id = d.id), 0) as total_amount,
					currency, description, deletion_mark, created_at, updated_at
				FROM doc_goods_receipts d
				WHERE deletion_mark = false
			`

			if filter.FromDate != nil {
				q += fmt.Sprintf(" AND date >= $%d", argIndex)
				args = append(args, *filter.FromDate)
				argIndex++
			}
			if filter.ToDate != nil {
				q += fmt.Sprintf(" AND date < $%d", argIndex)
				args = append(args, *filter.ToDate)
				argIndex++
			}
			if filter.Posted != nil {
				q += fmt.Sprintf(" AND posted = $%d", argIndex)
				args = append(args, *filter.Posted)
				argIndex++
			}

			unions = append(unions, q)

		case "goods_issue":
			q := `
				SELECT 
					id, 'goods_issue' as document_type, number, date,
					posted, organization_id,
					NULL::uuid as counterparty_id, '' as counterparty_name,
					warehouse_id, '' as warehouse_name,
					0.0 as total_quantity,
					COALESCE((SELECT SUM(amount) FROM doc_goods_issue_lines WHERE document_id = d.id), 0) as total_amount,
					currency, description, deletion_mark, created_at, updated_at
				FROM doc_goods_issues d
				WHERE deletion_mark = false
			`

			if filter.FromDate != nil {
				q += fmt.Sprintf(" AND date >= $%d", argIndex)
				args = append(args, *filter.FromDate)
				argIndex++
			}
			if filter.ToDate != nil {
				q += fmt.Sprintf(" AND date < $%d", argIndex)
				args = append(args, *filter.ToDate)
				argIndex++
			}
			if filter.Posted != nil {
				q += fmt.Sprintf(" AND posted = $%d", argIndex)
				args = append(args, *filter.Posted)
				argIndex++
			}

			unions = append(unions, q)

		}
	}

	if len(unions) == 0 {
		return &reports.DocumentJournal{
			Items:      []reports.DocumentJournalItem{},
			TotalCount: 0,
		}, nil
	}

	query := strings.Join(unions, " UNION ALL ")
	query += " ORDER BY date DESC, number"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	var items []reports.DocumentJournalItem
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &items, query, args...); err != nil {
		return nil, fmt.Errorf("document journal: %w", err)
	}

	return &reports.DocumentJournal{
		Items:      items,
		TotalCount: len(items),
		Limit:      filter.Limit,
		Offset:     filter.Offset,
	}, nil
}

// GetDocumentTypeSummary returns document counts and totals by type.
func (r *ReportRepo) GetDocumentTypeSummary(ctx context.Context, filter reports.DocumentJournalFilter) ([]reports.DocumentTypeSummary, error) {
	var result []reports.DocumentTypeSummary

	docTypes := filter.DocumentTypes
	if len(docTypes) == 0 {
		docTypes = []string{"goods_receipt", "goods_issue"}
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)

	for _, docType := range docTypes {
		var summary reports.DocumentTypeSummary
		summary.DocumentType = docType

		var query string
		var args []any
		argIndex := 1

		switch docType {
		case "goods_receipt":
			query = `
				SELECT 
					COUNT(*) as count,
					COUNT(*) FILTER (WHERE posted = true) as posted_count,
					0.0 as total_quantity,
					COALESCE(SUM((SELECT SUM(amount) FROM doc_goods_receipt_lines WHERE document_id = d.id)), 0) as total_amount
				FROM doc_goods_receipts d
				WHERE deletion_mark = false
			`
		case "goods_issue":
			query = `
				SELECT 
					COUNT(*) as count,
					COUNT(*) FILTER (WHERE posted = true) as posted_count,
					0.0 as total_quantity,
					COALESCE(SUM((SELECT SUM(amount) FROM doc_goods_issue_lines WHERE document_id = d.id)), 0) as total_amount
				FROM doc_goods_issues d
				WHERE deletion_mark = false
			`
		default:
			continue
		}

		if filter.FromDate != nil {
			query += fmt.Sprintf(" AND date >= $%d", argIndex)
			args = append(args, *filter.FromDate)
			argIndex++
		}
		if filter.ToDate != nil {
			query += fmt.Sprintf(" AND date < $%d", argIndex)
			args = append(args, *filter.ToDate)
			argIndex++
		}

		err := querier.QueryRow(ctx, query, args...).Scan(
			&summary.Count,
			&summary.PostedCount,
			&summary.TotalQuantity,
			&summary.TotalAmount,
		)
		if err != nil {
			return nil, fmt.Errorf("document type summary for %s: %w", docType, err)
		}

		result = append(result, summary)
	}

	return result, nil
}

// Ensure interface compliance
var _ reports.Repository = (*ReportRepo)(nil)
