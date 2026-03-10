package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/goods_issue"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	goodsIssuesTable     = "doc_goods_issues"
	goodsIssueLinesTable = "doc_goods_issue_lines"
)

// GoodsIssueRepo implements goods_issue.Repository.
// List() is inherited from BaseDocumentRepo (universal filter engine).
type GoodsIssueRepo struct {
	*BaseDocumentRepo[*goods_issue.GoodsIssue]
}

// NewGoodsIssueRepo creates a new goods issue repository.
func NewGoodsIssueRepo() *GoodsIssueRepo {
	repo := &GoodsIssueRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*goods_issue.GoodsIssue](
			goodsIssuesTable,
			postgres.ExtractDBColumns[goods_issue.GoodsIssue](),
			func() *goods_issue.GoodsIssue { return &goods_issue.GoodsIssue{} },
		),
	}

	repo.RegisterTablePart("lines", goodsIssueLinesTable, "document_id", []string{
		"product_id", "unit_id", "quantity", "unit_price",
		"discount_percent", "discount_amount",
		"vat_rate_id", "vat_amount", "amount",
	})

	// Register reference fields for deep filtering
	repo.RegisterReferenceField("customer_id", "cat_counterparties", "customer_id", []string{
		"inn", "kpp", "type", "legal_form", "full_name",
	})
	repo.RegisterReferenceField("warehouse_id", "cat_warehouses", "warehouse_id", []string{
		"type", "is_active", "allow_negative_stock",
	})
	repo.RegisterReferenceField("contract_id", "cat_contracts", "contract_id", []string{
		"type", "currency_id",
	})

	return repo
}

func (r *GoodsIssueRepo) GetLines(ctx context.Context, docID id.ID) ([]goods_issue.GoodsIssueLine, error) {
	q := r.Builder().
		Select(
			"line_id", "line_no", "product_id",
			"unit_id", "coefficient",
			"quantity", "unit_price",
			"discount_percent", "discount_amount",
			"vat_rate_id", "vat_amount", "amount",
		).
		From(goodsIssueLinesTable).
		Where(squirrel.Eq{"document_id": docID}).
		OrderBy("line_no")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var lines []goods_issue.GoodsIssueLine
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &lines, sql, args...); err != nil {
		return nil, fmt.Errorf("get lines: %w", err)
	}

	return lines, nil
}

func (r *GoodsIssueRepo) SaveLines(ctx context.Context, docID id.ID, lines []goods_issue.GoodsIssueLine) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	deleteSQL := "DELETE FROM " + goodsIssueLinesTable + " WHERE document_id = $1"
	if _, err := querier.Exec(ctx, deleteSQL, docID); err != nil {
		return fmt.Errorf("delete existing lines: %w", err)
	}

	if len(lines) == 0 {
		return nil
	}

	// Batch insert via COPY protocol (no 65,535 parameter limit).
	columns := []string{
		"line_id", "document_id", "line_no", "product_id",
		"unit_id", "coefficient",
		"quantity", "unit_price",
		"discount_percent", "discount_amount",
		"vat_rate_id", "vat_amount", "amount",
	}

	rows := make([][]any, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, []any{
			line.LineID, docID, line.LineNo, line.ProductID,
			line.UnitID, line.Coefficient,
			line.Quantity, line.UnitPrice,
			line.DiscountPercent, line.DiscountAmount,
			line.VATRateID, line.VATAmount, line.Amount,
		})
	}

	txm := r.getTxManager(ctx)
	inserter := postgres.NewBatchInserter(txm)
	if _, err := inserter.CopyFromSlice(ctx, goodsIssueLinesTable, columns, rows); err != nil {
		return fmt.Errorf("copy lines: %w", err)
	}

	return nil
}
