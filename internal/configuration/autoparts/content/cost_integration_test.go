//go:build integration

package content_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/goods_issue"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/goods_receipt"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/month_closing"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/inventory_cost"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/sales"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/document_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/posting"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// costFixture posts documents through their repositories, so their
// writes keep the cost sequence as the server's do.
type costFixture struct {
	*stockFixture
	other    id.ID
	receipts *document_repo.BaseDocumentRepo[*goods_receipt.GoodsReceipt]
	issues   *document_repo.BaseDocumentRepo[*goods_issue.GoodsIssue]
	closings *document_repo.BaseDocumentRepo[*month_closing.MonthClosing]
}

func newCostFixture(t *testing.T) *costFixture {
	f := &costFixture{
		stockFixture: newStockFixture(t), other: id.New(),
		receipts: document_repo.NewDocumentRepoForDefinition(goods_receipt.Definition, postgres.ExtractDBColumns[goods_receipt.GoodsReceipt](),
			func() *goods_receipt.GoodsReceipt { return new(goods_receipt.GoodsReceipt) }),
		issues: document_repo.NewDocumentRepoForDefinition(goods_issue.Definition, postgres.ExtractDBColumns[goods_issue.GoodsIssue](),
			func() *goods_issue.GoodsIssue { return new(goods_issue.GoodsIssue) }),
		closings: document_repo.NewDocumentRepoForDefinition(month_closing.Definition, postgres.ExtractDBColumns[month_closing.MonthClosing](),
			func() *month_closing.MonthClosing { return new(month_closing.MonthClosing) }),
	}
	_, err := f.db.Pool.Exec(f.ctx, `INSERT INTO cat_counterparties (id, code, name) VALUES ($1, 'CUS-2', 'СТО «Колесо»')`, f.other)
	require.NoError(t, err)
	return f
}

// store posts doc and writes it with repo: created the first time, updated
// when it is posted again.
func store[T posting.Postable](f *costFixture, repo *document_repo.BaseDocumentRepo[T], doc T) error {
	return f.db.RunInTransaction(f.ctx, func(ctx context.Context) error {
		created := doc.IsPosted()
		return f.engine.Post(ctx, doc, func(ctx context.Context) error {
			if created {
				return repo.Update(ctx, doc)
			}
			return repo.Create(ctx, doc)
		})
	})
}

func day(month time.Month, d int) time.Time { return time.Date(2026, month, d, 10, 0, 0, 0, time.UTC) }

func (f *costFixture) bought(t *testing.T, at time.Time, quantity int64, amount types.MinorUnits) {
	t.Helper()
	doc := f.receipt(goods_receipt.GoodsLine{ItemID: f.oil, Quantity: pieces(quantity), Amount: amount})
	doc.Number, doc.Date = "ПН-"+doc.ID.String()[24:], at
	require.NoError(t, store(f, f.receipts, doc))
}

func (f *costFixture) sold(t *testing.T, at time.Time, customer id.ID, quantity int64) {
	t.Helper()
	doc := f.issue(goods_issue.GoodsLine{ItemID: f.oil, Quantity: pieces(quantity), Amount: types.MinorUnits(quantity * 20000)})
	doc.Number, doc.Date, doc.CustomerID = "РН-"+doc.ID.String()[24:], at, customer
	require.NoError(t, store(f, f.issues, doc))
}

func (f *costFixture) closing(at time.Time) *month_closing.MonthClosing {
	doc := &month_closing.MonthClosing{Document: entity.NewDocument(), CurrencyID: f.currency}
	doc.Number, doc.Date = "ЗМ-"+doc.ID.String()[24:], at
	return doc
}

// stockCost is the cost register's balance of the oil at a moment.
func (f *costFixture) stockCost(t *testing.T, at time.Time) (types.Quantity, types.MinorUnits) {
	t.Helper()
	balances, err := inventory_cost.Register.BalancesAt(f.ctx, at, accumulation.Filter{"item_id": f.oil})
	require.NoError(t, err)
	if len(balances) == 0 {
		return 0, 0
	}
	require.Len(t, balances, 1)
	return balances[0].Quantity, balances[0].Amount
}

// costOfSales is the cost of the oil sold to each customer in a month.
func (f *costFixture) costOfSales(t *testing.T, month time.Month) map[id.ID]types.MinorUnits {
	t.Helper()
	rows, err := sales.Register.Turnovers(f.ctx, day(month, 1).Truncate(24*time.Hour), time.Date(2026, month+1, 1, 0, 0, 0, 0, time.UTC), accumulation.Filter{"item_id": f.oil})
	require.NoError(t, err)
	costs := map[id.ID]types.MinorUnits{}
	for _, row := range rows {
		if row.Cost != 0 {
			costs[row.CustomerID] += row.Cost
		}
	}
	return costs
}

func code(t *testing.T, err error) string {
	t.Helper()
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok, "%v", err)
	return appErr.Code
}

// The month closing costs the month's sales at the month's average cost
// of each item: the stock's cost at the month's start plus the month's
// purchases over their quantity. Each customer's sales take their share;
// selling all of the stock takes all of its cost. A document changed in a
// closed month moves the cost sequence back: the later closings wait until
// the month is closed again.
func TestMonthClosingCostsSalesAtTheMonthsAverage(t *testing.T) {
	f := newCostFixture(t)
	f.bought(t, day(10, 5), 10, 100000)  // 10 at 100.00
	f.bought(t, day(10, 20), 10, 130000) // 10 at 130.00
	f.sold(t, day(10, 25), f.supplier, 5)
	f.sold(t, day(10, 28), f.other, 10)

	other := f.closing(day(10, 31))
	other.CurrencyID = id.New()
	require.Equal(t, month_closing.CodeOtherCurrency, code(t, store(f, f.closings, other)), "the month's purchases are in another currency")
	october := f.closing(day(10, 31))
	require.NoError(t, store(f, f.closings, october))
	// 230 000 for 20: 11 500 each; 15 sold cost 172 500.
	require.Equal(t, map[id.ID]types.MinorUnits{f.supplier: 57500, f.other: 115000}, f.costOfSales(t, 10))
	quantity, cost := f.stockCost(t, day(11, 1).Truncate(24*time.Hour))
	require.Equal(t, pieces(5), quantity)
	require.Equal(t, types.MinorUnits(57500), cost)

	twice := f.closing(day(10, 15))
	require.Equal(t, month_closing.CodeAlreadyClosed, code(t, store(f, f.closings, twice)), "a month is closed once")

	f.bought(t, day(11, 3), 5, 75000)
	f.sold(t, day(11, 10), f.supplier, 10)
	november := f.closing(day(11, 30))
	require.NoError(t, store(f, f.closings, november))
	// 57 500 + 75 000 for 10, all sold: the whole cost.
	require.Equal(t, map[id.ID]types.MinorUnits{f.supplier: 132500}, f.costOfSales(t, 11))
	quantity, cost = f.stockCost(t, day(12, 1).Truncate(24*time.Hour))
	require.Zero(t, quantity)
	require.Zero(t, cost)

	// 2 more found on an October delivery: October is to be closed again,
	// and November waits for it.
	f.bought(t, day(10, 30), 2, 20000)
	require.Equal(t, posting.CodeSequenceNotRestored, code(t, store(f, f.closings, november)))

	require.NoError(t, store(f, f.closings, october))
	// 250 000 for 22; the 15 sold cost 170 455 (rounded), shared 5:10.
	require.Equal(t, map[id.ID]types.MinorUnits{f.supplier: 56818, f.other: 113637}, f.costOfSales(t, 10))
	// The refused posting rolled back; the server reads the document again.
	november, err := f.closings.GetByID(f.ctx, november.ID)
	require.NoError(t, err)
	require.NoError(t, store(f, f.closings, november))
	// 79 545 + 75 000 for 12; the 10 sold cost 128 788, 2 left at 25 757.
	require.Equal(t, map[id.ID]types.MinorUnits{f.supplier: 128788}, f.costOfSales(t, 11))
	quantity, cost = f.stockCost(t, day(12, 1).Truncate(24*time.Hour))
	require.Equal(t, pieces(2), quantity)
	require.Equal(t, types.MinorUnits(25757), cost)

	// Selling in a month that is not closed does not touch the sequence.
	f.sold(t, day(12, 2), f.other, 1)
	december := f.closing(day(12, 31))
	require.NoError(t, store(f, f.closings, december))
	require.Equal(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), f.boundary(t))

	// Unposted, the closing gives its month back.
	require.NoError(t, f.db.RunInTransaction(f.ctx, func(ctx context.Context) error {
		return f.engine.Unpost(ctx, december, func(ctx context.Context) error { return f.closings.Update(ctx, december) })
	}))
	require.Equal(t, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), f.boundary(t))
	require.Empty(t, f.costOfSales(t, 12))
}

// boundary is how far the cost sequence is restored.
func (f *costFixture) boundary(t *testing.T) time.Time {
	t.Helper()
	var at time.Time
	require.NoError(t, f.db.Pool.QueryRow(f.ctx, `SELECT boundary FROM sys_sequence_boundaries WHERE sequence = $1`, inventory_cost.Sequence).Scan(&at))
	return at.UTC()
}
