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
	return &GoodsReceiptRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*goods_receipt.GoodsReceipt](
			goodsReceiptsTable,
			postgres.ExtractDBColumns[goods_receipt.GoodsReceipt](),
			func() *goods_receipt.GoodsReceipt { return &goods_receipt.GoodsReceipt{} },
		),
	}
}

// GetLines retrieves lines for a goods receipt.
func (r *GoodsReceiptRepo) GetLines(ctx context.Context, docID id.ID) ([]goods_receipt.GoodsReceiptLine, error) {
	q := r.Builder().
		Select(
			"line_id", "line_no", "product_id",
			"unit_id", "coefficient",
			"quantity", "unit_price",
			"discount_percent", "discount_amount",
			"vat_rate_id", "vat_amount", "amount",
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

// SaveLines saves lines for a goods receipt (delete existing + insert new).
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

	// Insert new lines
	q := r.Builder().
		Insert(goodsReceiptLinesTable).
		Columns(
			"line_id", "document_id", "line_no", "product_id",
			"unit_id", "coefficient",
			"quantity", "unit_price",
			"discount_percent", "discount_amount",
			"vat_rate_id", "vat_amount", "amount",
		)

	for _, line := range lines {
		q = q.Values(
			line.LineID, docID, line.LineNo, line.ProductID,
			line.UnitID, line.Coefficient,
			line.Quantity, line.UnitPrice,
			line.DiscountPercent, line.DiscountAmount,
			line.VATRateID, line.VATAmount, line.Amount,
		)
	}

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build insert lines: %w", err)
	}

	if _, err := querier.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("insert lines: %w", err)
	}

	return nil
}
