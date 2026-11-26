//go:build integration

package register_repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/migration"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/register_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/testsupport"
)

// salesMovement has a reference and a text dimension and two resources, one
// of them money, so the shared balance trigger is exercised beyond the one
// dimension and resource of the reference configuration.
type salesMovement struct {
	entity.MovementBase
	CurrencyID id.ID            `db:"currency_id" meta:"label:Валюта,ref:currency" register:"dimension"`
	Channel    string           `db:"channel" meta:"label:Канал" register:"dimension"`
	Quantity   types.Quantity   `db:"quantity" meta:"label:Количество" register:"resource"`
	Amount     types.MinorUnits `db:"amount" meta:"label:Сумма" register:"resource,currency=currency_id"`
}

type salesLine struct {
	channel  string
	quantity int64
	amount   int64
	expense  bool
}

// salesDocument posts its lines to the register under test.
type salesDocument struct {
	entity.Document
	register *accumulation.Register[salesMovement]
	currency id.ID
	lines    []salesLine
}

func (d *salesDocument) GetDocumentType() string { return "SalesTest" }

func (d *salesDocument) RegisterMovements(_ context.Context, out *accumulation.Movements) error {
	for _, l := range d.lines {
		m := salesMovement{CurrencyID: d.currency, Channel: l.channel, Quantity: types.Quantity(l.quantity), Amount: types.MinorUnits(l.amount)}
		if l.expense {
			d.register.Expense(out, m)
		} else {
			d.register.Receipt(out, m)
		}
	}
	return nil
}

type accumulationFixture struct {
	ctx      context.Context
	tenant   *testsupport.SecurityTenant
	register *accumulation.Register[salesMovement]
	engine   *posting.Engine
	currency id.ID
}

func newAccumulationFixture(t *testing.T) *accumulationFixture {
	t.Helper()
	tenant := testsupport.NewSecurityTenants(t)[0]
	register := accumulation.New[salesMovement](accumulation.Definition{
		Key: "test_sales", Label: "Продажи (тест)", Table: "reg_test_sales", Kind: accumulation.Balances,
	})
	register.Bind(register_repo.NewAccumulationStore())
	f := &accumulationFixture{
		ctx:      tenant.Context(appctx.UserContext{UserID: id.New().String(), IsAdmin: true}),
		tenant:   tenant,
		register: register,
		engine:   posting.NewEngine(nil),
		currency: id.New(),
	}
	_, err := tenant.Pool.Exec(f.ctx, migration.AccumulationDDL(register.Schema()))
	require.NoError(t, err)
	accumulation.Install(f.engine, register)
	return f
}

func (f *accumulationFixture) document(date time.Time, lines ...salesLine) *salesDocument {
	doc := &salesDocument{Document: entity.NewDocument(), register: f.register, currency: f.currency, lines: lines}
	doc.Date = date
	return doc
}

func (f *accumulationFixture) post(t *testing.T, doc *salesDocument) {
	t.Helper()
	require.NoError(t, f.tenant.Tx.RunInTransaction(f.ctx, func(ctx context.Context) error {
		return f.engine.Post(ctx, doc, func(context.Context) error { return nil })
	}))
}

func (f *accumulationFixture) check(t *testing.T) error {
	t.Helper()
	db := stdlib.OpenDBFromPool(f.tenant.Pool)
	t.Cleanup(func() { _ = db.Close() })
	return migration.CheckAccumulationRegisters(f.ctx, db, []*accumulation.Schema{f.register.Schema()})
}

func byChannel(rows []salesMovement) map[string][2]int64 {
	result := map[string][2]int64{}
	for _, r := range rows {
		result[r.Channel] = [2]int64{int64(r.Quantity), int64(r.Amount)}
	}
	return result
}

// The tables AccumulationDDL creates match the declaration; postings keep
// the balances through the shared trigger, and the balances at a moment and
// the turnovers of a period are summed from the movements.
func TestDeclarativeRegisterKeepsBalances(t *testing.T) {
	f := newAccumulationFixture(t)
	require.NoError(t, f.check(t))

	day1 := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	receipt := f.document(day1, salesLine{"shop", 10, 1000, false}, salesLine{"web", 4, 400, false}, salesLine{"shop", 5, 500, false})
	f.post(t, receipt)
	sale := f.document(day2, salesLine{"shop", 3, 300, true}, salesLine{"web", 4, 400, true})
	f.post(t, sale)

	balances, err := f.register.Balances(f.ctx, nil)
	require.NoError(t, err)
	require.Equal(t, map[string][2]int64{"shop": {12, 1200}}, byChannel(balances), "web is sold out and left out")

	at, err := f.register.BalancesAt(f.ctx, day2, accumulation.Filter{"currency_id": f.currency})
	require.NoError(t, err)
	require.Equal(t, map[string][2]int64{"shop": {15, 1500}, "web": {4, 400}}, byChannel(at), "the sale of day 2 is not yet in")

	turnovers, err := f.register.Turnovers(f.ctx, day2, day2.AddDate(0, 0, 1), accumulation.Filter{"channel": []string{"shop", "web"}})
	require.NoError(t, err)
	require.Equal(t, map[string][2]int64{"shop": {-3, -300}, "web": {-4, -400}}, byChannel(turnovers))

	movements, err := f.register.GetDocumentMovements(f.ctx, sale.ID)
	require.NoError(t, err)
	require.Len(t, movements, 2)
	require.Equal(t, "test_sales", movements[0].PolicyEntity)
	require.Equal(t, string(entity.RecordTypeExpense), movements[0].RecordType)
	require.Equal(t, day2, movements[0].Period.UTC())
	require.Equal(t, "currency_id", movements[0].Columns[3].CurrencyRef)

	// Reposting replaces the sale's movements, unposting removes them.
	sale.lines = []salesLine{{"shop", 1, 100, true}}
	f.post(t, sale)
	balances, err = f.register.Balances(f.ctx, accumulation.Filter{"channel": "shop"})
	require.NoError(t, err)
	require.Equal(t, map[string][2]int64{"shop": {14, 1400}}, byChannel(balances))
	require.NoError(t, f.tenant.Tx.RunInTransaction(f.ctx, func(ctx context.Context) error {
		return f.engine.Unpost(ctx, sale, func(context.Context) error { return nil })
	}))
	balances, err = f.register.Balances(f.ctx, nil)
	require.NoError(t, err)
	require.Equal(t, map[string][2]int64{"shop": {15, 1500}, "web": {4, 400}}, byChannel(balances))

	_, err = f.register.Balances(f.ctx, accumulation.Filter{"quantity": 1})
	require.Error(t, err, "a resource is not a filter")

	_, err = f.tenant.Pool.Exec(f.ctx, `UPDATE reg_test_sales_movements SET quantity = 1`)
	require.ErrorContains(t, err, "are not updated")
}

// A migration that drifted from the declaration fails the check with every
// difference named.
func TestDeclarativeRegisterCheckFindsDrift(t *testing.T) {
	f := newAccumulationFixture(t)
	for _, statement := range []string{
		`ALTER TABLE reg_test_sales_movements ADD COLUMN note TEXT`,
		`ALTER TABLE reg_test_sales_balances ALTER COLUMN amount DROP NOT NULL`,
		`DROP TRIGGER trg_reg_test_sales_balances_delete ON reg_test_sales_movements`,
	} {
		_, err := f.tenant.Pool.Exec(f.ctx, statement)
		require.NoError(t, err, statement)
	}
	err := f.check(t)
	require.Error(t, err)
	require.ErrorContains(t, err, "reg_test_sales_movements has column note the register does not declare")
	require.ErrorContains(t, err, "reg_test_sales_balances.amount is nullable")
	require.ErrorContains(t, err, "no AFTER DELETE statement trigger")
	require.ErrorContains(t, err, "CREATE TABLE reg_test_sales_movements")
}
