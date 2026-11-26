//go:build integration

package register_repo_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/migration"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/register_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/testsupport"
)

// stockMovement keeps stock with its reserves, as УНФ's «Запасы»: the order
// dimension is empty (id.Nil) for the free balance and names the customer
// order a reserve is held for.
type stockMovement struct {
	entity.MovementBase
	ItemID   id.ID          `db:"item_id" meta:"label:Номенклатура,ref:item" register:"dimension"`
	OrderID  id.ID          `db:"order_id" meta:"label:Заказ покупателя,ref:customer_order" register:"dimension"`
	Quantity types.Quantity `db:"quantity" meta:"label:Количество" register:"resource"`
}

// stockLine moves quantity of the item under order; expense takes it away.
type stockLine struct {
	order    id.ID
	quantity int64
	expense  bool
}

type stockDocument struct {
	entity.Document
	register *accumulation.Register[stockMovement]
	item     id.ID
	lines    []stockLine
}

func (d *stockDocument) GetDocumentType() string { return "StockTest" }

func (d *stockDocument) RegisterMovements(_ context.Context, out *accumulation.Movements) error {
	for _, l := range d.lines {
		m := stockMovement{ItemID: d.item, OrderID: l.order, Quantity: types.Quantity(l.quantity)}
		if l.expense {
			d.register.Expense(out, m)
		} else {
			d.register.Receipt(out, m)
		}
	}
	return nil
}

// itemNames names the item for shortage messages, as the platform's
// reference resolver does.
type itemNames struct{ item id.ID }

func (n itemNames) ResolveRefs(_ context.Context, refs []domain.RefResolveRequest) ([]domain.RefResolveResult, error) {
	var result []domain.RefResolveResult
	for _, ref := range refs {
		if ref.RefType == "item" && ref.RefID == n.item {
			result = append(result, domain.RefResolveResult{RefType: ref.RefType, RefID: ref.RefID, Presentation: "Масло 5W-30"})
		}
	}
	return result, nil
}

type controlFixture struct {
	ctx      context.Context
	tenant   *testsupport.SecurityTenant
	register *accumulation.Register[stockMovement]
	engine   *posting.Engine
	item     id.ID
}

func newControlFixture(t *testing.T) *controlFixture {
	t.Helper()
	tenant := testsupport.NewSecurityTenants(t)[0]
	register := accumulation.New[stockMovement](accumulation.Definition{
		Key: "test_stock", Label: "Запасы (тест)", Table: "reg_test_stock", Kind: accumulation.Balances, NonNegative: true,
	})
	register.Bind(register_repo.NewAccumulationStore())
	f := &controlFixture{
		ctx:      tenant.Context(appctx.UserContext{UserID: id.New().String(), IsAdmin: true}),
		tenant:   tenant,
		register: register,
		engine:   posting.NewEngine(nil),
		item:     id.New(),
	}
	register.BindNames(itemNames{f.item})
	_, err := tenant.Pool.Exec(f.ctx, migration.AccumulationDDL(register.Schema()))
	require.NoError(t, err)
	accumulation.Install(f.engine, register)
	return f
}

func units(n int64) int64 { return n * types.QuantityScale }

func (f *controlFixture) document(lines ...stockLine) *stockDocument {
	doc := &stockDocument{Document: entity.NewDocument(), register: f.register, item: f.item, lines: lines}
	doc.Date = time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	return doc
}

func (f *controlFixture) post(doc *stockDocument) error {
	return f.tenant.Tx.RunInTransaction(f.ctx, func(ctx context.Context) error {
		return f.engine.Post(ctx, doc, func(context.Context) error { return nil })
	})
}

func (f *controlFixture) unpost(doc *stockDocument) error {
	return f.tenant.Tx.RunInTransaction(f.ctx, func(ctx context.Context) error {
		return f.engine.Unpost(ctx, doc, func(context.Context) error { return nil })
	})
}

// balance is the item's balance under order (id.Nil for the free one).
func (f *controlFixture) balance(t *testing.T, order id.ID) int64 {
	t.Helper()
	rows, err := f.register.Balances(f.ctx, accumulation.Filter{"item_id": f.item, "order_id": order})
	require.NoError(t, err)
	if len(rows) == 0 {
		return 0
	}
	return int64(rows[0].Quantity)
}

func shortages(t *testing.T, err error) []accumulation.Shortage {
	t.Helper()
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, accumulation.CodeInsufficientBalance, appErr.Code)
	list, ok := appErr.Details["shortages"].([]accumulation.Shortage)
	require.True(t, ok)
	return list
}

// A reserve moves stock from the free balance to the order's: the free stock
// the other customers may take shrinks, and the order's own reserve stays
// available to it. Nothing goes below zero.
func TestNonNegativeRegisterKeepsReserves(t *testing.T) {
	f := newControlFixture(t)
	order := id.New()
	free := id.Nil()

	receipt := f.document(stockLine{free, units(10), false})
	require.NoError(t, f.post(receipt))
	reserve := f.document(stockLine{free, units(7), true}, stockLine{order, units(7), false})
	require.NoError(t, f.post(reserve))
	require.Equal(t, units(3), f.balance(t, free))
	require.Equal(t, units(7), f.balance(t, order))

	// Another customer wants 5: only 3 are free.
	sale := f.document(stockLine{free, units(5), true})
	err := f.post(sale)
	require.Error(t, err)
	require.False(t, sale.IsPosted())
	require.Contains(t, err.Error(), "Недостаточно остатка")
	require.Contains(t, err.Error(), "Номенклатура «Масло 5W-30», Заказ покупателя «—»: Количество — в наличии 3.0000, требуется 5.0000, не хватает 2.0000")
	list := shortages(t, err)
	require.Len(t, list, 1)
	require.Equal(t, accumulation.Shortage{
		Register: "Запасы (тест)",
		Dimensions: []accumulation.DimensionValue{
			{Label: "Номенклатура", Value: "Масло 5W-30"}, {Label: "Заказ покупателя", Value: "—"},
		},
		Resource: "Количество", Available: "3.0000", Required: "5.0000", Result: "-2.0000",
	}, list[0])
	require.Equal(t, units(3), f.balance(t, free), "the refused posting changed nothing")

	// The order itself takes its reserve and the free rest.
	sale.lines = []stockLine{{order, units(7), true}, {free, units(3), true}}
	require.NoError(t, f.post(sale))
	require.Zero(t, f.balance(t, free))
	require.Zero(t, f.balance(t, order))

	// Taking the receipt back would leave the sale without stock.
	err = f.unpost(receipt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "остаток станет -10.0000")
	require.True(t, receipt.IsPosted())

	// Reposting the sale with less returns stock; then unposting the
	// reserve and the sale is fine, and so is the receipt.
	sale.lines = []stockLine{{order, units(2), true}}
	require.NoError(t, f.post(sale))
	require.Equal(t, units(5), f.balance(t, order))
	require.NoError(t, f.unpost(sale))
	require.NoError(t, f.unpost(reserve))
	require.NoError(t, f.unpost(receipt))
	require.Zero(t, f.balance(t, free))
}

// A balance that is already below zero, from before the control, does not
// block the receipt that raises it; only a posting that lowers a balance
// below zero is refused.
func TestNonNegativeRegisterLetsReceiptsRaiseANegativeBalance(t *testing.T) {
	f := newControlFixture(t)
	_, err := f.tenant.Pool.Exec(f.ctx, `INSERT INTO reg_test_stock_balances (item_id, order_id, quantity) VALUES ($1, $2, $3)`, f.item, id.Nil(), -units(4))
	require.NoError(t, err)
	require.NoError(t, f.post(f.document(stockLine{id.Nil(), units(1), false})))
	require.Equal(t, -units(3), f.balance(t, id.Nil()))
	require.Error(t, f.post(f.document(stockLine{id.Nil(), units(1), true})))
}

// Two sales of the last units at once: the register's locks make one wait
// for the other, which then sees the stock already gone.
func TestNonNegativeRegisterRefusesConcurrentOversell(t *testing.T) {
	f := newControlFixture(t)
	require.NoError(t, f.post(f.document(stockLine{id.Nil(), units(5), false})))

	sales := []*stockDocument{f.document(stockLine{id.Nil(), units(3), true}), f.document(stockLine{id.Nil(), units(3), true})}
	errs := make([]error, len(sales))
	var wg sync.WaitGroup
	for i, sale := range sales {
		wg.Go(func() { errs[i] = f.post(sale) })
	}
	wg.Wait()
	failed := 0
	for _, err := range errs {
		if err != nil {
			failed++
			require.Contains(t, err.Error(), "Недостаточно остатка")
		}
	}
	require.Equal(t, 1, failed, "%v", errs)
	require.Equal(t, units(2), f.balance(t, id.Nil()))
}
