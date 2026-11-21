//go:build integration

package content_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration"
	"metapus/internal/configuration/autoparts/content"
	"metapus/internal/configuration/autoparts/domain/documents/goods_receipt"
	"metapus/internal/configuration/autoparts/domain/registers/warehouse_stock"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
)

type stockFixture struct {
	ctx                  context.Context
	db                   *testsupport.SecurityTenant
	engine               *posting.Engine
	registers            *content.Registers
	supplier, currency   id.ID
	warehouse, oil, pads id.ID
}

// newStockFixture is a fresh tenant database of the auto parts
// configuration, migrated as the tenant command migrates it.
func newStockFixture(t *testing.T) *stockFixture {
	t.Helper()
	cfg := content.Autoparts()
	db := testsupport.NewSecurityTenants(t, configuration.MigrationOptions(cfg)...)[0]
	f := &stockFixture{
		ctx: db.Context(appctx.UserContext{UserID: id.New().String(), IsAdmin: true}), db: db,
		engine: posting.NewEngine(nil), registers: content.NewRegisters(),
		supplier: id.New(), currency: id.New(), warehouse: id.New(), oil: id.New(), pads: id.New(),
	}
	f.registers.InstallPosting(f.engine)
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

func (f *stockFixture) stock(t *testing.T, item id.ID) types.Quantity {
	t.Helper()
	balance, err := f.registers.WarehouseStock.Balance(f.ctx, f.warehouse, item)
	require.NoError(t, err)
	return balance.Quantity
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
	require.NoError(t, f.db.Tx.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Post(ctx, doc, noUpdate) }))
	require.True(t, doc.IsPosted())
	require.Equal(t, pieces(15), f.stock(t, f.oil))
	require.Equal(t, pieces(2), f.stock(t, f.pads))

	movements, err := f.registers.WarehouseStock.GetDocumentMovements(f.ctx, doc.ID)
	require.NoError(t, err)
	require.Len(t, movements, 3)
	for _, m := range movements {
		require.Equal(t, warehouse_stock.Name, m.PolicyEntity)
		require.Equal(t, "Запасы на складах", m.RegisterName)
		require.Equal(t, string(entity.RecordTypeReceipt), m.RecordType)
	}

	// The supplier delivered 8 bottles of oil after all: the repost replaces
	// the movements instead of adding to them.
	doc.Goods = doc.Goods[:2]
	doc.Goods[0].Quantity = pieces(8)
	require.NoError(t, f.db.Tx.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Post(ctx, doc, noUpdate) }))
	require.Equal(t, pieces(8), f.stock(t, f.oil))
	require.Equal(t, pieces(2), f.stock(t, f.pads))

	require.NoError(t, f.db.Tx.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Unpost(ctx, doc, noUpdate) }))
	require.False(t, doc.IsPosted())
	require.Zero(t, f.stock(t, f.oil))
	require.Zero(t, f.stock(t, f.pads))
	movements, err = f.registers.WarehouseStock.GetDocumentMovements(f.ctx, doc.ID)
	require.NoError(t, err)
	require.Empty(t, movements)
}

// A receipt without lines or warehouse is not posted and moves nothing.
func TestGoodsReceiptWithoutLinesIsNotPosted(t *testing.T) {
	f := newStockFixture(t)
	empty := f.receipt()
	require.Error(t, f.db.Tx.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Post(ctx, empty, noUpdate) }))
	negative := f.receipt(goods_receipt.GoodsLine{ItemID: f.oil, Quantity: pieces(-1)})
	require.Error(t, f.db.Tx.RunInTransaction(f.ctx, func(ctx context.Context) error { return f.engine.Post(ctx, negative, noUpdate) }))
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
