package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	goodsReceiptsTable     = "doc_goods_receipts"
	goodsReceiptLinesTable = "doc_goods_receipt_lines"
)

// GoodsReceiptRepo implements goods_receipt.Repository.
// List() is inherited from BaseDocumentRepo (universal filter engine).
type GoodsReceiptRepo struct {
	*BaseDocumentRepo[*goods_receipt.GoodsReceipt]
}

// NewGoodsReceiptRepo creates a new goods receipt repository.
func NewGoodsReceiptRepo() *GoodsReceiptRepo {
	repo := &GoodsReceiptRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*goods_receipt.GoodsReceipt](
			goodsReceiptsTable,
			postgres.ExtractDBColumns[goods_receipt.GoodsReceipt](),
			func() *goods_receipt.GoodsReceipt { return &goods_receipt.GoodsReceipt{} },
		),
	}

	// Register table part "lines" for filtering by tabular section columns.
	// Column names match DB columns in doc_goods_receipt_lines.
	repo.RegisterTablePart("lines", goodsReceiptLinesTable, "document_id", []string{
		"product_id", "unit_id", "quantity", "unit_price",
		"discount_percent", "discount_amount",
		"vat_rate_id", "vat_percent", "vat_amount", "amount",
	})

	// Register reference fields for deep filtering
	repo.RegisterReferenceField("supplier_id", "cat_counterparties", "supplier_id", []string{
		"inn", "kpp", "type", "legal_form", "full_name",
	})
	repo.RegisterReferenceField("warehouse_id", "cat_warehouses", "warehouse_id", []string{
		"type", "is_active", "allow_negative_stock",
	})
	repo.RegisterReferenceField("contract_id", "cat_contracts", "contract_id", []string{
		"type", "currency_id",
	})

	// Register RLS dimensions for DataScope filtering.
	repo.RegisterRLSDimension("organization", "organization_id")

	return repo
}

// GetLines retrieves lines for a goods receipt.
func (r *GoodsReceiptRepo) GetLines(ctx context.Context, docID id.ID) ([]goods_receipt.GoodsReceiptLine, error) {
	q := r.Builder().
		Select(
			"line_id", "line_no", "product_id",
			"unit_id", "coefficient",
			"quantity", "unit_price",
			"discount_percent", "discount_amount",
			"vat_rate_id", "vat_percent", "vat_amount", "amount",
		).
		From(goodsReceiptLinesTable).
		Where(squirrel.Eq{"document_id": docID}).
		OrderBy("line_no")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var lines []goods_receipt.GoodsReceiptLine
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &lines, sql, args...); err != nil {
		return nil, fmt.Errorf("get lines: %w", err)
	}

	return lines, nil
}

// SaveLines saves lines for a goods receipt (delete existing + COPY new).
// Uses PostgreSQL COPY protocol to avoid the 65,535 parameter limit
// and for higher throughput on large tabular sections.
func (r *GoodsReceiptRepo) SaveLines(ctx context.Context, docID id.ID, lines []goods_receipt.GoodsReceiptLine) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	// Delete existing lines
	deleteSQL := "DELETE FROM " + goodsReceiptLinesTable + " WHERE document_id = $1"
	if _, err := querier.Exec(ctx, deleteSQL, docID); err != nil {
		return fmt.Errorf("delete existing lines: %w", err)
	}

	if len(lines) == 0 {
		return nil
	}

	// Batch insert new lines via COPY protocol.
	columns := []string{
		"line_id", "document_id", "line_no", "product_id",
		"unit_id", "coefficient",
		"quantity", "unit_price",
		"discount_percent", "discount_amount",
		"vat_rate_id", "vat_percent", "vat_amount", "amount",
	}

	rows := make([][]any, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, []any{
			line.LineID, docID, line.LineNo, line.ProductID,
			line.UnitID, line.Coefficient,
			line.Quantity, line.UnitPrice,
			line.DiscountPercent, line.DiscountAmount,
			line.VATRateID, line.VATPercent, line.VATAmount, line.Amount,
		})
	}

	txm := r.getTxManager(ctx)
	inserter := postgres.NewBatchInserter(txm)
	if _, err := inserter.CopyFromSlice(ctx, goodsReceiptLinesTable, columns, rows); err != nil {
		return fmt.Errorf("copy lines: %w", err)
	}

	return nil
}
