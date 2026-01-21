package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	goodsReceiptsTable     = "doc_goods_receipts"
	goodsReceiptLinesTable = "doc_goods_receipt_lines"
)

// GoodsReceiptRepo implements goods_receipt.Repository.
type GoodsReceiptRepo struct {
	*BaseDocumentRepo[*goods_receipt.GoodsReceipt]
}

// NewGoodsReceiptRepo creates a new goods receipt repository.
func NewGoodsReceiptRepo() *GoodsReceiptRepo {
	return &GoodsReceiptRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*goods_receipt.GoodsReceipt](
			goodsReceiptsTable,
			postgres.ExtractDBColumns[goods_receipt.GoodsReceipt](),
		),
	}
}

// GetLines retrieves lines for a goods receipt.
func (r *GoodsReceiptRepo) GetLines(ctx context.Context, docID id.ID) ([]goods_receipt.GoodsReceiptLine, error) {
	q := r.Builder().
		Select(
			"line_id", "line_no", "product_id",
			"quantity", "unit_price", "vat_rate", "vat_amount", "amount",
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
			"quantity", "unit_price", "vat_rate", "vat_amount", "amount",
		)

	for _, line := range lines {
		q = q.Values(
			line.LineID, docID, line.LineNo, line.ProductID,
			line.Quantity, line.UnitPrice, line.VATRate, line.VATAmount, line.Amount,
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

// List retrieves goods receipts with filtering.
func (r *GoodsReceiptRepo) List(ctx context.Context, filter goods_receipt.ListFilter) (domain.ListResult[*goods_receipt.GoodsReceipt], error) {
	result := domain.ListResult[*goods_receipt.GoodsReceipt]{
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}

	q := r.baseSelect(ctx)

	if !filter.IncludeDeleted {
		q = q.Where(squirrel.Eq{"deletion_mark": false})
	}

	if filter.SupplierID != nil {
		q = q.Where(squirrel.Eq{"supplier_id": *filter.SupplierID})
	}

	if filter.WarehouseID != nil {
		q = q.Where(squirrel.Eq{"warehouse_id": *filter.WarehouseID})
	}

	if filter.Posted != nil {
		q = q.Where(squirrel.Eq{"posted": *filter.Posted})
	}

	if filter.DateFrom != nil {
		q = q.Where(squirrel.GtOrEq{"date": *filter.DateFrom})
	}

	if filter.DateTo != nil {
		q = q.Where(squirrel.LtOrEq{"date": *filter.DateTo})
	}

	if filter.Search != "" {
		searchPattern := "%" + filter.Search + "%"
		q = q.Where(squirrel.Or{
			squirrel.ILike{"number": searchPattern},
			squirrel.ILike{"supplier_doc_number": searchPattern},
		})
	}

	countQ := r.Builder().Select("COUNT(*)").FromSelect(q, "sub")
	countSQL, countArgs, err := countQ.ToSql()
	if err != nil {
		return result, fmt.Errorf("build count: %w", err)
	}

	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := querier.QueryRow(ctx, countSQL, countArgs...).Scan(&result.TotalCount); err != nil {
		return result, fmt.Errorf("count: %w", err)
	}

	orderBy := "date DESC"
	if filter.OrderBy != "" {
		orderBy = filter.OrderBy
	}
	q = q.OrderBy(orderBy)

	if filter.Limit > 0 {
		q = q.Limit(uint64(filter.Limit))
	}
	if filter.Offset > 0 {
		q = q.Offset(uint64(filter.Offset))
	}

	sql, args, err := q.ToSql()
	if err != nil {
		return result, fmt.Errorf("build query: %w", err)
	}

	if err := pgxscan.Select(ctx, querier, &result.Items, sql, args...); err != nil {
		return result, fmt.Errorf("select: %w", err)
	}

	return result, nil
}
