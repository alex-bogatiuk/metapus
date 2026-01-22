package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/documents/goods_issue"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	goodsIssuesTable     = "doc_goods_issues"
	goodsIssueLinesTable = "doc_goods_issue_lines"
)

// GoodsIssueRepo implements goods_issue.Repository.
type GoodsIssueRepo struct {
	*BaseDocumentRepo[*goods_issue.GoodsIssue]
}

// NewGoodsIssueRepo creates a new goods issue repository.
func NewGoodsIssueRepo() *GoodsIssueRepo {
	return &GoodsIssueRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*goods_issue.GoodsIssue](
			goodsIssuesTable,
			postgres.ExtractDBColumns[goods_issue.GoodsIssue](),
			func() *goods_issue.GoodsIssue { return &goods_issue.GoodsIssue{} },
		),
	}
}

func (r *GoodsIssueRepo) GetLines(ctx context.Context, docID id.ID) ([]goods_issue.GoodsIssueLine, error) {
	q := r.Builder().
		Select(
			"line_id", "line_no", "product_id",
			"quantity", "unit_price", "vat_rate", "vat_amount", "amount",
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

	q := r.Builder().
		Insert(goodsIssueLinesTable).
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

func (r *GoodsIssueRepo) List(ctx context.Context, filter goods_issue.ListFilter) (domain.ListResult[*goods_issue.GoodsIssue], error) {
	result := domain.ListResult[*goods_issue.GoodsIssue]{
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}

	q := r.baseSelect(ctx)

	if !filter.IncludeDeleted {
		q = q.Where(squirrel.Eq{"deletion_mark": false})
	}

	if filter.CustomerID != nil {
		q = q.Where(squirrel.Eq{"customer_id": *filter.CustomerID})
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
			squirrel.ILike{"customer_order_number": searchPattern},
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
