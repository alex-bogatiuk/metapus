//go:build integration

package content_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/content"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/catalogs/order_state"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/customer_order"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/goods_issue"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/goods_receipt"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/documents/price_setting"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/customer_orders"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/item_prices"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/order_state_history"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/stock"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/warehouse_stock"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/infrastructure/storage/postgres/order_state_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/appctx"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/document_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/posting"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers/information"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

type stockFixture struct {
	ctx                  context.Context
	db                   *sdktest.Tenant
	engine               *posting.Engine
	supplier, currency   id.ID
	warehouse, oil, pads id.ID
}

// newStockFixture is a fresh tenant database of the auto parts
// configuration, migrated as the tenant command migrates it: the migration
// also checks the declared register's tables. The engine posts the register
// as the server does.
func newStockFixture(t *testing.T) *stockFixture {
	t.Helper()
	db := sdktest.NewTenant(t, content.Autoparts())
	f := &stockFixture{
		ctx: db.AdminContext(), db: db,
		engine:   posting.NewEngine(),
		supplier: id.New(), currency: id.New(), warehouse: id.New(), oil: id.New(), pads: id.New(),
	}
	declared := []accumulation.Declared{warehouse_stock.Register, stock.Register, customer_orders.Register}
	for _, r := range declared {
		r.Bind(registers.NewAccumulationStore())
	}
	accumulation.Install(f.engine, declared...)
	for _, r := range []information.Declared{order_state_history.Register, item_prices.Register} {
		r.Bind(registers.NewInformationStore())
	}
	information.Install(f.engine, item_prices.Register)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO cat_counterparties (id, code, name) VALUES ($1, 'SUP-1', 'Дистрибьютор')`, []any{f.supplier}},
		{`INSERT INTO cat_currencies (id, code, name, iso_code, symbol, decimal_places, minor_multiplier) VALUES ($1, 'RUB', 'Рубль', 'RUB', '₽', 2, 100)`, []any{f.currency}},
		{`INSERT INTO cat_warehouses (id, code, name) VALUES ($1, 'WH-1', 'Основной склад')`, []any{f.warehouse}},
		{`INSERT INTO cat_items (id, code, name, article, unit) VALUES ($1, 'IT-1', 'Масло 5W-30', '152648', 'шт.'), ($2, 'IT-2', 'Колодки', '0 986 494 524', 'комплект')`, []any{f.oil, f.pads}},
	} {
		_, err := db.Pool.Exec(f.ctx, statement.sql, statement.args...)
		require.NoError(t, err, statement.sql)
	}
	return f
}

func (f *stockFixture) receipt(lines ...goods_receipt.GoodsLine) *goods_receipt.GoodsReceipt {
	doc := &goods_receipt.GoodsReceipt{Document: entity.NewDocument(), SupplierID: f.supplier, WarehouseID: f.warehouse, CurrencyID: f.currency}
	doc.Date = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	for i, line := range lines {
		line.LineID, line.LineNo = id.New(), i+1
		doc.Goods = append(doc.Goods, line)
	}
	return doc
}

func (f *stockFixture) issue(lines ...goods_issue.GoodsLine) *goods_issue.GoodsIssue {
	doc := &goods_issue.GoodsIssue{Document: entity.NewDocument(), CustomerID: f.supplier, WarehouseID: f.warehouse, CurrencyID: f.currency}
	doc.Date = time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	for i, line := range lines {
		line.LineID, line.LineNo = id.New(), i+1
		doc.Goods = append(doc.Goods, line)
	}
	return doc
}

// order is an order in progress; the engine posts it without the service's
// hook, so the status is set as ApplyState would set it for «В работе».
func (f *stockFixture) order(lines ...customer_order.GoodsLine) *customer_order.CustomerOrder {
	working := order_state.Working
	doc := &customer_order.CustomerOrder{Document: entity.NewDocument(), CustomerID: f.supplier, WarehouseID: f.warehouse, CurrencyID: f.currency,
		StateID: &working, Status: order_state.InProgress}
	doc.Date = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	for i, line := range lines {
		line.LineID, line.LineNo = id.New(), i+1
		doc.Goods = append(doc.Goods, line)
	}
	return doc
}

// reserved is the item's stock under order (stock.Free() for the free one).
func (f *stockFixture) reserved(t *testing.T, item, order id.ID) types.Quantity {
	t.Helper()
	balances, err := stock.Register.Balances(f.ctx, accumulation.Filter{"warehouse_id": f.warehouse, "item_id": item, "customer_order_id": order})
	require.NoError(t, err)
	if len(balances) == 0 {
		return 0
	}
	return balances[0].Quantity
}

// awaited is what the order still awaits of the item.
func (f *stockFixture) awaited(t *testing.T, item, order id.ID) types.Quantity {
	t.Helper()
	balances, err := customer_orders.Register.Balances(f.ctx, accumulation.Filter{"customer_order_id": order, "item_id": item})
	require.NoError(t, err)
	if len(balances) == 0 {
		return 0
	}
	return balances[0].Quantity
}

func (f *stockFixture) post(doc posting.Postable) error {
	return f.db.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Post(ctx, doc, noUpdate) })
}

func (f *stockFixture) stock(t *testing.T, item id.ID) types.Quantity {
	t.Helper()
	balances, err := warehouse_stock.Register.Balances(f.ctx, accumulation.Filter{"warehouse_id": f.warehouse, "item_id": item})
	require.NoError(t, err)
	if len(balances) == 0 {
		return 0
	}
	require.Len(t, balances, 1)
	return balances[0].Quantity
}

func pieces(n int64) types.Quantity { return types.NewQuantityFromInt64Scaled(n * types.QuantityScale) }

func noUpdate(context.Context) error { return nil }

// Posting a goods receipt adds its lines to the warehouse stock; reposting
// replaces them and unposting takes them away again.
func TestGoodsReceiptMovesWarehouseStock(t *testing.T) {
	f := newStockFixture(t)
	doc := f.receipt(
		goods_receipt.GoodsLine{ItemID: f.oil, Quantity: pieces(10), Price: 90000, Amount: 900000},
		goods_receipt.GoodsLine{ItemID: f.pads, Quantity: pieces(2), Price: 350000, Amount: 700000},
		goods_receipt.GoodsLine{ItemID: f.oil, Quantity: pieces(5), Price: 90000, Amount: 450000},
	)
	require.NoError(t, f.db.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Post(ctx, doc, noUpdate) }))
	require.True(t, doc.IsPosted())
	require.Equal(t, pieces(15), f.stock(t, f.oil))
	require.Equal(t, pieces(2), f.stock(t, f.pads))

	movements, err := warehouse_stock.Register.GetDocumentMovements(f.ctx, doc.ID)
	require.NoError(t, err)
	require.Len(t, movements, 3)
	for _, m := range movements {
		require.Equal(t, warehouse_stock.Register.Key(), m.PolicyEntity)
		require.Equal(t, "Запасы на складах", m.RegisterName)
		require.Equal(t, string(entity.RecordTypeReceipt), m.RecordType)
	}

	// The supplier delivered 8 bottles of oil after all: the repost replaces
	// the movements instead of adding to them.
	doc.Goods = doc.Goods[:2]
	doc.Goods[0].Quantity = pieces(8)
	require.NoError(t, f.db.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Post(ctx, doc, noUpdate) }))
	require.Equal(t, pieces(8), f.stock(t, f.oil))
	require.Equal(t, pieces(2), f.stock(t, f.pads))

	require.NoError(t, f.db.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Unpost(ctx, doc, noUpdate) }))
	require.False(t, doc.IsPosted())
	require.Zero(t, f.stock(t, f.oil))
	require.Zero(t, f.stock(t, f.pads))
	movements, err = warehouse_stock.Register.GetDocumentMovements(f.ctx, doc.ID)
	require.NoError(t, err)
	require.Empty(t, movements)
}

// A receipt without lines or warehouse is not posted and moves nothing.
func TestGoodsReceiptWithoutLinesIsNotPosted(t *testing.T) {
	f := newStockFixture(t)
	empty := f.receipt()
	require.Error(t, f.db.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Post(ctx, empty, noUpdate) }))
	negative := f.receipt(goods_receipt.GoodsLine{ItemID: f.oil, Quantity: pieces(-1)})
	require.Error(t, f.db.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Post(ctx, negative, noUpdate) }))
	require.Zero(t, f.stock(t, f.oil))
}

// The tenant database holds the auto parts tables and no crypto table.
func TestAutopartsTenantHasNoCryptoTables(t *testing.T) {
	f := newStockFixture(t)
	var autoparts, crypto bool
	require.NoError(t, f.db.Pool.QueryRow(f.ctx, `SELECT to_regclass('reg_warehouse_stock_balances') IS NOT NULL, to_regclass('cat_merchants') IS NOT NULL`).Scan(&autoparts, &crypto))
	require.True(t, autoparts)
	require.False(t, crypto)
}

// A goods issue takes its lines from the warehouse stock, and the stock never
// goes below zero: an issue of more than the warehouse holds is refused, and
// so is reposting or unposting the receipt the issued goods came with.
func TestGoodsIssueCannotTakeMoreThanTheWarehouseHolds(t *testing.T) {
	f := newStockFixture(t)
	receipt := f.receipt(goods_receipt.GoodsLine{ItemID: f.oil, Quantity: pieces(10), Price: 90000, Amount: 900000})
	require.NoError(t, f.post(receipt))

	sale := f.issue(goods_issue.GoodsLine{ItemID: f.oil, Quantity: pieces(4), Price: 120000, Amount: 480000})
	require.NoError(t, f.post(sale))
	require.Equal(t, pieces(6), f.stock(t, f.oil))

	tooMuch := f.issue(
		goods_issue.GoodsLine{ItemID: f.oil, Quantity: pieces(7), Price: 120000, Amount: 840000},
		goods_issue.GoodsLine{ItemID: f.pads, Quantity: pieces(1), Price: 450000, Amount: 450000},
	)
	err := f.post(tooMuch)
	require.Error(t, err)
	require.False(t, tooMuch.IsPosted())
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, accumulation.CodeInsufficientBalance, appErr.Code)
	shortages := appErr.Details["shortages"].([]accumulation.Shortage)
	require.Len(t, shortages, 4, "both lines are short in the warehouse and in the free stock: the oil by one, the pads entirely")
	require.Contains(t, appErr.Message, "в наличии 6.0000, требуется 7.0000, не хватает 1.0000")
	require.Equal(t, pieces(6), f.stock(t, f.oil), "the refused issue moved nothing")

	// The supplier's receipt cannot shrink below what was sold from it.
	receipt.Goods[0].Quantity = pieces(3)
	require.Error(t, f.post(receipt))
	require.Error(t, f.db.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Unpost(ctx, receipt, noUpdate) }))
	require.Equal(t, pieces(6), f.stock(t, f.oil))

	// Once the sale is unposted the receipt may go.
	require.NoError(t, f.db.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Unpost(ctx, sale, noUpdate) }))
	require.NoError(t, f.db.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Unpost(ctx, receipt, noUpdate) }))
	require.Zero(t, f.stock(t, f.oil))
}

// The order scenario of the requirements: an order reserves what is in
// stock, the rest arrives under the order and is reserved on arrival, the
// order ships from its reserve. Other customers cannot take the reserve, and
// an order cannot be shipped more than it ordered.
func TestCustomerOrderReservesStockForTheCustomer(t *testing.T) {
	f := newStockFixture(t)
	require.NoError(t, f.post(f.receipt(goods_receipt.GoodsLine{ItemID: f.oil, Quantity: pieces(10), Price: 90000, Amount: 900000})))

	// The customer orders 6, 4 of them from stock; the rest is bought.
	order := f.order(customer_order.GoodsLine{ItemID: f.oil, Quantity: pieces(6), Reserve: pieces(4), Price: 120000, Amount: 720000})
	require.NoError(t, f.post(order))
	require.Equal(t, pieces(6), f.reserved(t, f.oil, stock.Free()))
	require.Equal(t, pieces(4), f.reserved(t, f.oil, order.ID))
	require.Equal(t, pieces(6), f.awaited(t, f.oil, order.ID))
	require.Equal(t, pieces(10), f.stock(t, f.oil), "a reserve does not move goods in the warehouse")

	// Another order cannot reserve more than is free.
	greedy := f.order(customer_order.GoodsLine{ItemID: f.oil, Quantity: pieces(8), Reserve: pieces(8)})
	require.Error(t, f.post(greedy))

	// A sale to someone else cannot take the reserve: 10 are on the shelf
	// but only 6 are free.
	walkIn := f.issue(goods_issue.GoodsLine{ItemID: f.oil, Quantity: pieces(7), Price: 120000, Amount: 840000})
	err := f.post(walkIn)
	require.Error(t, err)
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Len(t, appErr.Details["shortages"].([]accumulation.Shortage), 1, "the warehouse holds 10; only the free stock is short")
	require.Equal(t, "Запасы", appErr.Details["shortages"].([]accumulation.Shortage)[0].Register)

	// The missing 2 arrive for the order and are reserved for it.
	forOrder := f.receipt(goods_receipt.GoodsLine{ItemID: f.oil, Quantity: pieces(2), Price: 90000, Amount: 180000, CustomerOrderID: &order.ID})
	require.NoError(t, f.post(forOrder))
	require.Equal(t, pieces(6), f.reserved(t, f.oil, order.ID))
	require.Equal(t, pieces(6), f.reserved(t, f.oil, stock.Free()))

	// The order ships from its reserve and is closed.
	shipment := f.issue(goods_issue.GoodsLine{ItemID: f.oil, Quantity: pieces(6), Reserve: pieces(6), Price: 120000, Amount: 720000})
	shipment.CustomerOrderID = &order.ID
	require.NoError(t, f.post(shipment))
	require.Zero(t, f.reserved(t, f.oil, order.ID))
	require.Zero(t, f.awaited(t, f.oil, order.ID))
	require.Equal(t, pieces(6), f.stock(t, f.oil))

	// Nothing more is shipped under the closed order, and the order cannot
	// be unposted once shipped.
	extra := f.issue(goods_issue.GoodsLine{ItemID: f.oil, Quantity: pieces(1), Price: 120000, Amount: 120000})
	extra.CustomerOrderID = &order.ID
	require.Error(t, f.post(extra))
	require.Error(t, f.db.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Unpost(ctx, order, noUpdate) }))

	// A reserve is taken only under an order.
	noOrder := f.issue(goods_issue.GoodsLine{ItemID: f.oil, Quantity: pieces(1), Reserve: pieces(1)})
	require.ErrorContains(t, f.post(noOrder), "a reserve is taken under a customer order")

	// The free rest sells to anyone.
	require.NoError(t, f.post(f.issue(goods_issue.GoodsLine{ItemID: f.oil, Quantity: pieces(6), Price: 120000, Amount: 720000})))
	require.Zero(t, f.stock(t, f.oil))
	require.Zero(t, f.reserved(t, f.oil, stock.Free()))
}

// A goods issue filled from the order ships what the order still awaits,
// from its reserve first; a goods receipt filled for the order buys what is
// not reserved, under the order.
func TestDocumentsFilledFromACustomerOrder(t *testing.T) {
	f := newStockFixture(t)
	require.NoError(t, f.post(f.receipt(goods_receipt.GoodsLine{ItemID: f.oil, Quantity: pieces(10)})))
	order := f.order(customer_order.GoodsLine{ItemID: f.oil, Quantity: pieces(6), Reserve: pieces(4), Price: 120000, Amount: 720000})

	// An unposted order awaits nothing.
	require.ErrorContains(t, goods_issue.FromCustomerOrder(f.ctx, order, &goods_issue.GoodsIssue{}), "nothing left to ship")
	require.NoError(t, f.post(order))

	purchase := &goods_receipt.GoodsReceipt{}
	require.NoError(t, goods_receipt.ForCustomerOrder(f.ctx, order, purchase))
	require.Equal(t, f.warehouse, purchase.WarehouseID)
	require.Len(t, purchase.Goods, 1)
	require.Equal(t, pieces(2), purchase.Goods[0].Quantity, "6 ordered, 4 reserved: 2 to buy")
	require.Equal(t, order.ID, *purchase.Goods[0].CustomerOrderID)

	issue := &goods_issue.GoodsIssue{}
	require.NoError(t, goods_issue.FromCustomerOrder(f.ctx, order, issue))
	require.Equal(t, order.ID, *issue.CustomerOrderID)
	require.Equal(t, f.supplier, issue.CustomerID)
	require.Len(t, issue.Goods, 1)
	require.Equal(t, pieces(6), issue.Goods[0].Quantity)
	require.Equal(t, pieces(4), issue.Goods[0].Reserve)
	require.Equal(t, order.Goods[0].Amount, issue.Goods[0].Amount)

	// The purchase arrives: everything is reserved, nothing is left to buy.
	purchase.Document, purchase.SupplierID = entity.NewDocument(), f.supplier
	purchase.Date = time.Date(2026, 11, 21, 15, 0, 0, 0, time.UTC)
	require.NoError(t, f.post(purchase))
	require.ErrorContains(t, goods_receipt.ForCustomerOrder(f.ctx, order, &goods_receipt.GoodsReceipt{}), "everything the customer order awaits is reserved")

	// Half is shipped; the next issue takes the other half, all from reserve.
	half := f.issue(goods_issue.GoodsLine{ItemID: f.oil, Quantity: pieces(3), Reserve: pieces(3), Price: 120000, Amount: 360000})
	half.CustomerOrderID = &order.ID
	require.NoError(t, f.post(half))
	rest := &goods_issue.GoodsIssue{}
	require.NoError(t, goods_issue.FromCustomerOrder(f.ctx, order, rest))
	require.Len(t, rest.Goods, 1)
	require.Equal(t, pieces(3), rest.Goods[0].Quantity)
	require.Equal(t, pieces(3), rest.Goods[0].Reserve)
	require.Equal(t, types.MinorUnits(360000), rest.Goods[0].Amount, "the amount follows the quantity")
}

// The order's state decides what it does: a new order reserves nothing, an
// order in progress reserves and awaits, and closing it (completed or
// cancelled) gives the unshipped reserve back to the free stock and awaits
// nothing more. Reopening restores it.
func TestCustomerOrderStateDecidesItsMovements(t *testing.T) {
	f := newStockFixture(t)
	require.NoError(t, f.post(f.receipt(goods_receipt.GoodsLine{ItemID: f.oil, Quantity: pieces(10)})))
	order := f.order(customer_order.GoodsLine{ItemID: f.oil, Quantity: pieces(6), Reserve: pieces(4)})
	save := func(state id.ID) {
		t.Helper()
		order.StateID = &state
		require.NoError(t, customer_order.ApplyState(f.ctx, order_state_repo.New(), order))
		require.NoError(t, f.post(order))
	}

	// A new order (no state given) is open: posted, it moves nothing.
	order.StateID = nil
	require.NoError(t, customer_order.ApplyState(f.ctx, order_state_repo.New(), order))
	require.Equal(t, order_state.New, *order.StateID)
	require.Equal(t, order_state.Open, order.Status)
	require.NoError(t, f.post(order))
	require.Equal(t, pieces(10), f.reserved(t, f.oil, stock.Free()))
	require.Zero(t, f.awaited(t, f.oil, order.ID))

	save(order_state.Working)
	require.Equal(t, order_state.InProgress, order.Status)
	require.Equal(t, pieces(4), f.reserved(t, f.oil, order.ID))
	require.Equal(t, pieces(6), f.awaited(t, f.oil, order.ID))

	// 2 more arrive for the order; 3 are shipped from the reserve.
	require.NoError(t, f.post(f.receipt(goods_receipt.GoodsLine{ItemID: f.oil, Quantity: pieces(2), CustomerOrderID: &order.ID})))
	shipment := f.issue(goods_issue.GoodsLine{ItemID: f.oil, Quantity: pieces(3), Reserve: pieces(3)})
	shipment.CustomerOrderID = &order.ID
	require.NoError(t, f.post(shipment))
	require.Equal(t, pieces(3), f.reserved(t, f.oil, order.ID))
	require.Equal(t, pieces(3), f.awaited(t, f.oil, order.ID))

	// The customer takes no more: the order is completed. The 3 not shipped
	// are cancelled and their reserve is free again.
	save(order_state.Done)
	require.Equal(t, order_state.Completed, order.Status)
	require.Equal(t, pieces(3), order.Goods[0].Cancelled)
	require.Equal(t, pieces(3), order.Goods[0].Released)
	require.Zero(t, f.awaited(t, f.oil, order.ID))
	require.Zero(t, f.reserved(t, f.oil, order.ID))
	require.Equal(t, pieces(9), f.reserved(t, f.oil, stock.Free()))
	require.Equal(t, pieces(9), f.stock(t, f.oil))

	// Saving a closed order again changes nothing.
	save(order_state.Done)
	require.Equal(t, pieces(3), order.Goods[0].Cancelled)
	require.Equal(t, pieces(3), order.Goods[0].Released)
	require.Equal(t, pieces(9), f.reserved(t, f.oil, stock.Free()))

	// Nothing more ships under a closed order, and it cannot be unposted
	// once shipped from.
	more := f.issue(goods_issue.GoodsLine{ItemID: f.oil, Quantity: pieces(1)})
	more.CustomerOrderID = &order.ID
	require.Error(t, f.post(more))
	require.Error(t, f.db.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Unpost(ctx, order, noUpdate) }))

	// Reopened, it awaits the rest and takes its reserve back.
	save(order_state.ReadyForPickup)
	require.Zero(t, order.Goods[0].Cancelled)
	require.Equal(t, pieces(3), f.awaited(t, f.oil, order.ID))
	require.Equal(t, pieces(3), f.reserved(t, f.oil, order.ID))
	require.Equal(t, pieces(6), f.reserved(t, f.oil, stock.Free()))

	// Cancelled, it lets everything go.
	save(order_state.Rejected)
	require.Equal(t, order_state.Cancelled, order.Status)
	require.Zero(t, f.awaited(t, f.oil, order.ID))
	require.Zero(t, f.reserved(t, f.oil, order.ID))
	require.Equal(t, pieces(9), f.reserved(t, f.oil, stock.Free()))

	// A state that is not in the catalog is refused.
	unknown := id.New()
	order.StateID = &unknown
	require.ErrorContains(t, customer_order.ApplyState(f.ctx, order_state_repo.New(), order), "the order state is not found")
}

// A price setting records item prices from its date; an order line without
// a price takes the price valid on the order's date.
func TestOrderLinesTakeThePriceList(t *testing.T) {
	f := newStockFixture(t)
	setting := &price_setting.PriceSetting{Document: entity.NewDocument(), CurrencyID: f.currency,
		Prices: []price_setting.PriceLine{{LineID: id.New(), LineNo: 1, ItemID: f.oil, Price: 95000}, {LineID: id.New(), LineNo: 2, ItemID: f.pads, Price: 410000}}}
	setting.Date = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, f.post(setting))
	raise := &price_setting.PriceSetting{Document: entity.NewDocument(), CurrencyID: f.currency,
		Prices: []price_setting.PriceLine{{LineID: id.New(), LineNo: 1, ItemID: f.oil, Price: 99000}}}
	raise.Date = time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	require.NoError(t, f.post(raise))

	order := f.order(
		customer_order.GoodsLine{ItemID: f.oil, Quantity: pieces(2)},
		customer_order.GoodsLine{ItemID: f.pads, Quantity: pieces(1), Price: 380000, Amount: 380000},
	)
	order.Date = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	require.NoError(t, customer_order.FillPrices(f.ctx, order))
	require.Equal(t, types.MinorUnits(95000), order.Goods[0].Price, "the price valid on the order's date, not the later one")
	require.Equal(t, types.MinorUnits(190000), order.Goods[0].Amount)
	require.Equal(t, types.MinorUnits(380000), order.Goods[1].Price, "a line's own price stays")

	// Reposting the setting replaces its prices; unposting removes them.
	setting.Prices = setting.Prices[:1]
	setting.Prices[0].Price = 97000
	require.NoError(t, f.post(setting))
	later := f.order(customer_order.GoodsLine{ItemID: f.oil, Quantity: pieces(1)}, customer_order.GoodsLine{ItemID: f.pads, Quantity: pieces(1)})
	later.Date = order.Date
	require.NoError(t, customer_order.FillPrices(f.ctx, later))
	require.Equal(t, types.MinorUnits(97000), later.Goods[0].Price)
	require.Zero(t, later.Goods[1].Price, "the pads have no price any more")

	duplicate := &price_setting.PriceSetting{Document: entity.NewDocument(), CurrencyID: f.currency,
		Prices: []price_setting.PriceLine{{LineNo: 1, ItemID: f.oil, Price: 1}, {LineNo: 2, ItemID: f.oil, Price: 2}}}
	duplicate.Date = setting.Date
	require.ErrorContains(t, f.post(duplicate), "already has a price in line 1")
}

// The order's state changes are kept in the history, once each.
func TestOrderStateHistory(t *testing.T) {
	f := newStockFixture(t)
	order := f.order(customer_order.GoodsLine{ItemID: f.oil, Quantity: pieces(1)})
	at := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	set := func(state id.ID, minutes int) {
		t.Helper()
		order.StateID = &state
		require.NoError(t, customer_order.ApplyState(f.ctx, order_state_repo.New(), order))
		require.NoError(t, customer_order.RecordState(f.ctx, order, at.Add(time.Duration(minutes)*time.Minute)))
	}
	set(order_state.New, 0)
	set(order_state.New, 5)
	set(order_state.Working, 10)
	set(order_state.ReadyForPickup, 20)

	history, err := order_state_history.Register.Records(f.ctx, information.Filter{"customer_order_id": order.ID}, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Len(t, history, 3, "saving the same state again writes nothing")
	require.Equal(t, order_state.New, history[0].StateID)
	require.Equal(t, order_state.InProgress, history[1].Status)
	require.NotEqual(t, id.Nil(), history[2].AuthorID, "the author is the user who saved the order")

	then, err := order_state_history.Register.SliceLast(f.ctx, at.Add(15*time.Minute), information.Filter{"customer_order_id": order.ID})
	require.NoError(t, err)
	require.Equal(t, order_state.Working, then[0].StateID, "the state at a moment is the last one set before it")
}

// The order's status model holds when the order is written: closing an
// order is open to everyone who may change it, reopening a closed one only
// to holders of document:customer_order:reopen, and each change of status
// is in the order's status history.
func TestCustomerOrderStatusFollowsItsModel(t *testing.T) {
	f := newStockFixture(t)
	repo := document_repo.NewDocumentRepoForDefinition(customer_order.Definition, postgres.ExtractDBColumns[customer_order.CustomerOrder](),
		func() *customer_order.CustomerOrder { return new(customer_order.CustomerOrder) })
	clerk := f.db.Context(appctx.UserContext{UserID: id.New().String()})
	manager := f.db.Context(appctx.UserContext{UserID: id.New().String(), Permissions: []string{"document:customer_order:reopen"}})
	order := f.order(customer_order.GoodsLine{ItemID: f.oil, Quantity: pieces(2)})
	order.Number = "ЗП-" + order.ID.String()[24:]
	stored := false
	write := func(ctx context.Context, state id.ID) error {
		order.StateID = &state
		return f.db.RunInTransaction(ctx, func(ctx context.Context) error {
			if err := customer_order.ApplyState(ctx, order_state_repo.New(), order); err != nil {
				return err
			}
			if !stored {
				stored = true
				return repo.Create(ctx, order)
			}
			return repo.Update(ctx, order)
		})
	}
	require.NoError(t, write(clerk, order_state.Working))

	require.NoError(t, write(clerk, order_state.Done))
	require.Equal(t, order_state.Completed, order.Status)
	err := write(clerk, order_state.ReadyForPickup)
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, apperror.CodeForbidden, appErr.Code, "reopening a closed order needs its permission")
	require.NoError(t, write(manager, order_state.ReadyForPickup))
	require.Equal(t, order_state.InProgress, order.Status)
	require.NoError(t, write(clerk, order_state.AwaitingPayment), "an order in work goes back to open")

	var trail []string
	rows, err := f.db.Pool.Query(f.ctx, `SELECT status FROM sys_document_status_history WHERE document_type = 'customer_order' AND document_id = $1 ORDER BY changed_at, id`, order.ID)
	require.NoError(t, err)
	for rows.Next() {
		var status string
		require.NoError(t, rows.Scan(&status))
		trail = append(trail, status)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"in_progress", "completed", "in_progress", "open"}, trail)
}
