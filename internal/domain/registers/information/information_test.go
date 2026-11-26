package information_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/information"
)

type price struct {
	information.RecordBase
	ItemID     id.ID            `db:"item_id" meta:"label:Номенклатура,ref:item" register:"dimension"`
	Kind       string           `db:"kind" register:"dimension"`
	Price      types.MinorUnits `db:"price" meta:"label:Цена" register:"resource,currency=currency_id"`
	CurrencyID id.ID            `db:"currency_id" meta:"ref:currency" register:"resource"`
	Active     bool             `db:"active" register:"resource"`
}

func TestSchemaFollowsTheRecordType(t *testing.T) {
	t.Parallel()
	schema, err := information.NewSchema(information.Definition{
		Key: "prices", Label: "Цены", Table: "reg_prices", Periodicity: information.Day, Mode: information.Recorder,
	}, reflect.TypeFor[price]())
	require.NoError(t, err)
	require.Equal(t, []string{"period", "recorder_id", "recorder_type", "item_id", "kind", "price", "currency_id", "active"}, schema.Columns())
	require.Equal(t, []string{"period", "item_id", "kind"}, schema.KeyColumns())
	require.Equal(t, information.FieldFlag, schema.Resources[2].Type)
	require.Equal(t, time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC), schema.PeriodOf(time.Date(2026, 11, 26, 23, 30, 0, 0, time.UTC)))

	schema, err = information.NewSchema(information.Definition{
		Key: "prices", Label: "Цены", Table: "reg_prices", Periodicity: information.NonPeriodic, Mode: information.Independent,
	}, reflect.TypeFor[price]())
	require.NoError(t, err)
	require.Equal(t, []string{"item_id", "kind", "price", "currency_id", "active"}, schema.Columns())
	require.Equal(t, []string{"item_id", "kind"}, schema.KeyColumns())
}

type wrongRecord struct {
	Amount types.MinorUnits `db:"amount" register:"resource"`
	Flag   bool             `db:"flag" register:"dimension"`
	Note   string           `db:"note"`
}

func TestSchemaNamesEveryMistake(t *testing.T) {
	t.Parallel()
	_, err := information.NewSchema(information.Definition{Key: "X", Table: "prices", Periodicity: "hour", Mode: "free"}, reflect.TypeFor[wrongRecord]())
	for _, want := range []string{
		`key "X" is not snake case`, "the label is empty", `table "prices"`, `periodicity "hour"`, `mode "free"`,
		"amount amount names no currency field", "dimension Flag has type bool", "field Note has a column but no register",
		"does not embed information.RecordBase",
	} {
		require.ErrorContains(t, err, want)
	}
	require.Panics(t, func() { information.New[wrongRecord](information.Definition{Key: "x"}) })
}
