package accumulation_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/accumulation"
)

type stockMovement struct {
	entity.MovementBase
	WarehouseID id.ID            `db:"warehouse_id" meta:"label:Склад,ref:warehouse" register:"dimension"`
	Lot         string           `db:"lot" register:"dimension"`
	Quantity    types.Quantity   `db:"quantity" meta:"label:Количество" register:"resource"`
	Cost        types.MinorUnits `db:"cost" meta:"label:Себестоимость" register:"resource,currency=currency_id"`
	CurrencyID  id.ID            `db:"currency_id" meta:"ref:currency" register:"dimension"`
}

var stockDefinition = accumulation.Definition{Key: "stock", Label: "Запасы", Table: "reg_stock", Kind: accumulation.Balances}

func TestSchemaFollowsTheMovementType(t *testing.T) {
	t.Parallel()
	schema, err := accumulation.NewSchema(stockDefinition, reflect.TypeFor[stockMovement]())
	require.NoError(t, err)
	require.Equal(t, "reg_stock_movements", schema.MovementsTable())
	require.Equal(t, "reg_stock_balances", schema.BalancesTable())
	require.Equal(t, []string{"warehouse_id", "lot", "currency_id"}, schema.DimensionColumns())
	require.Equal(t, []string{"quantity", "cost"}, schema.ResourceColumns())
	require.Equal(t, accumulation.FieldRef, schema.Dimensions[0].Type)
	require.Equal(t, "warehouse", schema.Dimensions[0].Ref)
	require.Equal(t, "Склад", schema.Dimensions[0].Label)
	require.Equal(t, accumulation.FieldText, schema.Dimensions[1].Type)
	require.Equal(t, "Lot", schema.Dimensions[1].Label, "a field without a label is named after itself")
	require.Equal(t, accumulation.FieldAmount, schema.Resources[1].Type)
	require.Equal(t, "currency_id", schema.Resources[1].Currency)
	require.Equal(t, append(accumulation.HeaderColumns(), "warehouse_id", "lot", "currency_id", "quantity", "cost"), schema.MovementColumns())

	turnovers := stockDefinition
	turnovers.Kind = accumulation.Turnovers
	schema, err = accumulation.NewSchema(turnovers, reflect.TypeFor[stockMovement]())
	require.NoError(t, err)
	require.Empty(t, schema.BalancesTable())
}

type wrongMovement struct {
	Warehouse id.ID            `db:"warehouse" register:"dimension"`
	Price     float64          `db:"price" register:"resource"`
	Amount    types.MinorUnits `db:"amount" register:"resource"`
	Note      string           `db:"note"`
	Period    string           `db:"period" register:"dimension"`
}

func TestSchemaNamesEveryMistake(t *testing.T) {
	t.Parallel()
	_, err := accumulation.NewSchema(accumulation.Definition{Key: "Stock", Table: "stock", Kind: "levels"}, reflect.TypeFor[wrongMovement]())
	require.Error(t, err)
	for _, want := range []string{
		`key "Stock" is not snake case`,
		"the label is empty",
		`table "stock" does not start with reg_`,
		`kind "levels"`,
		"dimension Warehouse refers to nothing",
		"resource Price has type float64",
		"amount resource amount names no currency dimension",
		"field Note has a column but no register",
		"column period of field Period is taken",
		"does not embed entity.MovementBase",
	} {
		require.ErrorContains(t, err, want)
	}
}

func TestNewPanicsOnAWrongDeclaration(t *testing.T) {
	t.Parallel()
	require.Panics(t, func() { accumulation.New[wrongMovement](stockDefinition) })
}

type receipt struct {
	entity.Document
	register  *accumulation.Register[stockMovement]
	warehouse id.ID
	quantity  int64
	ownPeriod time.Time
}

func (r *receipt) GetDocumentType() string { return "Receipt" }

func (r *receipt) RegisterMovements(_ context.Context, out *accumulation.Movements) error {
	r.register.Receipt(out, stockMovement{WarehouseID: r.warehouse, Quantity: types.Quantity(r.quantity)})
	r.register.Expense(out, stockMovement{MovementBase: entity.MovementBase{Period: r.ownPeriod}, WarehouseID: r.warehouse, Quantity: 1})
	return nil
}

// The visitor fills each movement's recorder, the next posted version and
// the document's date, keeps a period the movement sets, and collects once
// however often it is installed.
func TestVisitorStampsTheDocumentsMovements(t *testing.T) {
	t.Parallel()
	register := accumulation.New[stockMovement](stockDefinition)
	doc := &receipt{Document: entity.NewDocument(), register: register, warehouse: id.New(), quantity: 7, ownPeriod: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	doc.Date = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	set := posting.NewMovementSet()
	require.NoError(t, accumulation.Visitor{}.CollectMovements(context.Background(), doc, set))
	require.NoError(t, accumulation.Visitor{}.CollectMovements(context.Background(), doc, set))

	raw, ok := set.GetExtension("stock")
	require.True(t, ok)
	movements := raw.([]stockMovement)
	require.Len(t, movements, 2)
	require.Equal(t, doc.ID, movements[0].RecorderID)
	require.Equal(t, "Receipt", movements[0].RecorderType)
	require.Equal(t, doc.PostedVersion+1, movements[0].RecorderVersion)
	require.Equal(t, entity.RecordTypeReceipt, movements[0].RecordType)
	require.Equal(t, doc.Date, movements[0].Period)
	require.Equal(t, entity.RecordTypeExpense, movements[1].RecordType)
	require.Equal(t, doc.ownPeriod, movements[1].Period)
}

func TestTurnoversRegisterRefusesExpenses(t *testing.T) {
	t.Parallel()
	def := stockDefinition
	def.Kind = accumulation.Turnovers
	register := accumulation.New[stockMovement](def)
	doc := &receipt{Document: entity.NewDocument(), register: register, warehouse: id.New(), quantity: 1}
	err := accumulation.Visitor{}.CollectMovements(context.Background(), doc, posting.NewMovementSet())
	require.ErrorContains(t, err, "turnovers register stock has no expenses")

	_, err = register.Balances(context.Background(), nil)
	require.Error(t, err)
}

func TestUnboundRegisterSaysSo(t *testing.T) {
	t.Parallel()
	register := accumulation.New[stockMovement](stockDefinition)
	_, err := register.Balances(context.Background(), nil)
	require.ErrorContains(t, err, "not bound to storage")
	_, err = register.Balances(context.Background(), accumulation.Filter{"quantity": 1})
	require.ErrorContains(t, err, "has no dimension quantity")
}

func TestOnlyBalancesAreKeptNonNegative(t *testing.T) {
	t.Parallel()
	def := stockDefinition
	def.Kind, def.NonNegative = accumulation.Turnovers, true
	_, err := accumulation.NewSchema(def, reflect.TypeFor[stockMovement]())
	require.ErrorContains(t, err, "a turnovers register has no balances to keep non-negative")
}
