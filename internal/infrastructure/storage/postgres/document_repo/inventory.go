package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/inventory"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	inventoriesTable    = "doc_inventories"
	inventoryLinesTable = "doc_inventory_lines"
)

// InventoryRepo implements inventory.Repository.
type InventoryRepo struct {
	*BaseDocumentRepo[*inventory.Inventory]
}

// NewInventoryRepo creates a new inventory repository.
func NewInventoryRepo() *InventoryRepo {
	return &InventoryRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*inventory.Inventory](
			inventoriesTable,
			postgres.ExtractDBColumns[inventory.Inventory](),
			func() *inventory.Inventory { return &inventory.Inventory{} },
		),
	}
}

func (r *InventoryRepo) GetLines(ctx context.Context, docID id.ID) ([]inventory.InventoryLine, error) {
	q := r.Builder().
		Select(
			"line_id", "line_no", "product_id",
			"book_quantity", "actual_quantity", "deviation",
			"unit_price", "deviation_amount",
			"counted", "counted_at", "counted_by",
		).
		From(inventoryLinesTable).
		Where(squirrel.Eq{"document_id": docID}).
		OrderBy("line_no")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var lines []inventory.InventoryLine
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &lines, sql, args...); err != nil {
		return nil, fmt.Errorf("get lines: %w", err)
	}

	return lines, nil
}

func (r *InventoryRepo) SaveLines(ctx context.Context, docID id.ID, lines []inventory.InventoryLine) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	deleteSQL := "DELETE FROM " + inventoryLinesTable + " WHERE document_id = $1"
	if _, err := querier.Exec(ctx, deleteSQL, docID); err != nil {
		return fmt.Errorf("delete existing lines: %w", err)
	}

	if len(lines) == 0 {
		return nil
	}

	q := r.Builder().
		Insert(inventoryLinesTable).
		Columns(
			"line_id", "document_id", "line_no", "product_id",
			"book_quantity", "actual_quantity",
			"unit_price", "counted", "counted_at", "counted_by",
		)

	for _, line := range lines {
		q = q.Values(
			line.LineID, docID, line.LineNo, line.ProductID,
			line.BookQuantity, line.ActualQuantity,
			line.UnitPrice, line.Counted, line.CountedAt, line.CountedBy,
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

func (r *InventoryRepo) List(ctx context.Context, filter inventory.ListFilter) (domain.ListResult[*inventory.Inventory], error) {
	result := domain.ListResult[*inventory.Inventory]{
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}

	q := r.baseSelect(ctx)

	if !filter.IncludeDeleted {
		q = q.Where(squirrel.Eq{"deletion_mark": false})
	}

	if filter.WarehouseID != nil {
		q = q.Where(squirrel.Eq{"warehouse_id": *filter.WarehouseID})
	}

	if filter.Status != nil {
		q = q.Where(squirrel.Eq{"status": *filter.Status})
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
		q = q.Where(squirrel.ILike{"number": searchPattern})
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

var _ inventory.Repository = (*InventoryRepo)(nil)
