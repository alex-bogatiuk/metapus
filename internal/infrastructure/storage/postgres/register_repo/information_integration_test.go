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
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/information"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/migration"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/register_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/testsupport"
)

// stateRecord is an independent register kept to the second: the state of
// an order from a moment on.
type stateRecord struct {
	information.RecordBase
	OrderID id.ID  `db:"order_id" meta:"label:Заказ,ref:order" register:"dimension"`
	State   string `db:"state" meta:"label:Состояние" register:"resource"`
	Urgent  bool   `db:"urgent" meta:"label:Срочный" register:"resource"`
}

// priceRecord is a register of daily prices that documents record.
type priceRecord struct {
	information.RecordBase
	ItemID     id.ID            `db:"item_id" meta:"label:Номенклатура,ref:item" register:"dimension"`
	Price      types.MinorUnits `db:"price" meta:"label:Цена" register:"resource,currency=currency_id"`
	CurrencyID id.ID            `db:"currency_id" meta:"label:Валюта,ref:currency" register:"resource"`
}

type priceDocument struct {
	entity.Document
	register *information.Register[priceRecord]
	currency id.ID
	prices   map[id.ID]int64
}

func (d *priceDocument) GetDocumentType() string { return "PriceTest" }

func (d *priceDocument) RegisterMovements(_ context.Context, out *accumulation.Movements) error {
	for item, price := range d.prices {
		d.register.Record(out, priceRecord{ItemID: item, Price: types.MinorUnits(price), CurrencyID: d.currency})
	}
	return nil
}

type informationFixture struct {
	ctx            context.Context
	tenant         *testsupport.SecurityTenant
	states         *information.Register[stateRecord]
	prices         *information.Register[priceRecord]
	engine         *posting.Engine
	currency, item id.ID
}

func newInformationFixture(t *testing.T) *informationFixture {
	t.Helper()
	tenant := testsupport.NewSecurityTenants(t)[0]
	f := &informationFixture{
		ctx:    tenant.Context(appctx.UserContext{UserID: id.New().String(), IsAdmin: true}),
		tenant: tenant,
		states: information.New[stateRecord](information.Definition{
			Key: "test_order_states", Label: "Состояния (тест)", Table: "reg_test_order_states", Periodicity: information.Second, Mode: information.Independent,
		}),
		prices: information.New[priceRecord](information.Definition{
			Key: "test_prices", Label: "Цены (тест)", Table: "reg_test_prices", Periodicity: information.Day, Mode: information.Recorder,
		}),
		engine:   posting.NewEngine(nil),
		currency: id.New(), item: id.New(),
	}
	for _, schema := range []*information.Schema{f.states.Schema(), f.prices.Schema()} {
		_, err := tenant.Pool.Exec(f.ctx, migration.InformationDDL(schema))
		require.NoError(t, err)
	}
	f.states.Bind(register_repo.NewInformationStore())
	f.prices.Bind(register_repo.NewInformationStore())
	information.Install(f.engine, f.states, f.prices)
	return f
}

func (f *informationFixture) check(t *testing.T) error {
	t.Helper()
	db := stdlib.OpenDBFromPool(f.tenant.Pool)
	t.Cleanup(func() { _ = db.Close() })
	return migration.CheckInformationRegisters(f.ctx, db, []*information.Schema{f.states.Schema(), f.prices.Schema()})
}

func (f *informationFixture) post(doc *priceDocument) error {
	return f.tenant.Tx.RunInTransaction(f.ctx, func(ctx context.Context) error {
		return f.engine.Post(ctx, doc, func(context.Context) error { return nil })
	})
}

// An independent register keeps a record per moment; the slice of the
// latest records at a moment gives each order's state then. Setting the same
// key again replaces the record, Delete removes it.
func TestIndependentInformationRegisterKeepsHistory(t *testing.T) {
	f := newInformationFixture(t)
	require.NoError(t, f.check(t))
	orderA, orderB := id.New(), id.New()
	t0 := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	for _, r := range []stateRecord{
		{RecordBase: information.RecordBase{Period: t0}, OrderID: orderA, State: "new"},
		{RecordBase: information.RecordBase{Period: t0.Add(time.Hour)}, OrderID: orderA, State: "in_progress", Urgent: true},
		{RecordBase: information.RecordBase{Period: t0.Add(30 * time.Minute)}, OrderID: orderB, State: "new"},
	} {
		require.NoError(t, f.states.Set(f.ctx, r))
	}
	slice := func(at time.Time) map[id.ID]string {
		rows, err := f.states.SliceLast(f.ctx, at, nil)
		require.NoError(t, err)
		result := map[id.ID]string{}
		for _, r := range rows {
			result[r.OrderID] = r.State
		}
		return result
	}
	require.Equal(t, map[id.ID]string{orderA: "new"}, slice(t0.Add(10*time.Minute)))
	require.Equal(t, map[id.ID]string{orderA: "in_progress", orderB: "new"}, slice(t0.Add(2*time.Hour)))

	// The same moment again replaces the record.
	require.NoError(t, f.states.Set(f.ctx, stateRecord{RecordBase: information.RecordBase{Period: t0.Add(time.Hour)}, OrderID: orderA, State: "ready"}))
	history, err := f.states.Records(f.ctx, information.Filter{"order_id": orderA}, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "ready", history[1].State)
	require.False(t, history[1].Urgent)

	require.NoError(t, f.states.Delete(f.ctx, stateRecord{RecordBase: information.RecordBase{Period: t0.Add(time.Hour)}, OrderID: orderA}))
	require.Equal(t, map[id.ID]string{orderA: "new", orderB: "new"}, slice(t0.Add(2*time.Hour)))

	require.Error(t, f.states.Set(f.ctx, stateRecord{OrderID: orderA, State: "new"}), "a periodic record needs its period")
	_, err = f.states.SliceLast(f.ctx, t0, information.Filter{"state": "new"})
	require.Error(t, err, "a resource is not a filter")
}

// A recorder register is written by posting: the document's date is the
// period, reposting replaces the records, unposting removes them. Two
// documents cannot set the same item's price on the same day.
func TestRecorderInformationRegisterFollowsItsDocuments(t *testing.T) {
	f := newInformationFixture(t)
	day1 := time.Date(2026, 11, 20, 15, 30, 0, 0, time.UTC)
	doc := &priceDocument{Document: entity.NewDocument(), register: f.prices, currency: f.currency, prices: map[id.ID]int64{f.item: 120000}}
	doc.Date = day1
	require.NoError(t, f.post(doc))
	later := &priceDocument{Document: entity.NewDocument(), register: f.prices, currency: f.currency, prices: map[id.ID]int64{f.item: 135000}}
	later.Date = day1.AddDate(0, 0, 5)
	require.NoError(t, f.post(later))

	priceAt := func(at time.Time) int64 {
		rows, err := f.prices.SliceLast(f.ctx, at, information.Filter{"item_id": f.item})
		require.NoError(t, err)
		if len(rows) == 0 {
			return 0
		}
		return int64(rows[0].Price)
	}
	require.Zero(t, priceAt(day1.AddDate(0, 0, -1)))
	require.Equal(t, int64(120000), priceAt(day1), "a day register keeps the day, not the hour")
	require.Equal(t, int64(120000), priceAt(day1.AddDate(0, 0, 4)))
	require.Equal(t, int64(135000), priceAt(day1.AddDate(0, 0, 30)))

	movements, err := f.prices.GetDocumentMovements(f.ctx, doc.ID)
	require.NoError(t, err)
	require.Len(t, movements, 1)
	require.Equal(t, "Цены (тест)", movements[0].RegisterName)
	require.Equal(t, "currency_id", movements[0].Columns[1].CurrencyRef)

	doc.prices[f.item] = 125000
	require.NoError(t, f.post(doc))
	require.Equal(t, int64(125000), priceAt(day1.AddDate(0, 0, 1)), "reposting replaces the records")

	clash := &priceDocument{Document: entity.NewDocument(), register: f.prices, currency: f.currency, prices: map[id.ID]int64{f.item: 1}}
	clash.Date = day1.Add(time.Hour)
	require.ErrorContains(t, f.post(clash), "already has a record for this period")

	require.NoError(t, f.tenant.Tx.RunInTransaction(f.ctx, func(ctx context.Context) error {
		return f.engine.Unpost(ctx, doc, func(context.Context) error { return nil })
	}))
	require.Zero(t, priceAt(day1.AddDate(0, 0, 1)))
	require.Error(t, f.prices.Set(f.ctx, priceRecord{ItemID: f.item}), "documents write a recorder register")
}

func TestInformationRegisterCheckFindsDrift(t *testing.T) {
	f := newInformationFixture(t)
	for _, statement := range []string{
		`ALTER TABLE reg_test_prices ADD COLUMN note TEXT`,
		`ALTER TABLE reg_test_order_states ALTER COLUMN urgent DROP NOT NULL`,
	} {
		_, err := f.tenant.Pool.Exec(f.ctx, statement)
		require.NoError(t, err, statement)
	}
	err := f.check(t)
	require.ErrorContains(t, err, "reg_test_prices has column note the register does not declare")
	require.ErrorContains(t, err, "reg_test_order_states.urgent is nullable")
	require.ErrorContains(t, err, "CREATE TABLE reg_test_prices")
}
