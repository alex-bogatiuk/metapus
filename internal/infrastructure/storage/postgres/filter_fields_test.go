package postgres_test

import (
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	"github.com/alex-bogatiuk/metapus/internal/domain/filter"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/stretchr/testify/require"
)

func TestFilterMetadataUsesStorageTypesAndColumnWhitelist(t *testing.T) {
	type model struct {
		entity.BaseCatalog
		Amount    *types.MinorUnits `db:"total_amount"`
		Quantity  types.Quantity    `db:"quantity"`
		Count     int64             `db:"count"`
		Date      *time.Time        `db:"date"`
		NotStored types.MinorUnits  `db:"-"`
	}
	fields := postgres.ExtractFilterFields[*model]("id", "total_amount", "quantity", "count", "date")
	require.Equal(t, map[string]filter.FieldSpec{
		"id":           {Kind: filter.NativeValue},
		"count":        {Kind: filter.NativeValue},
		"total_amount": {Kind: filter.MoneyValue},
		"quantity":     {Kind: filter.QuantityValue},
		"date":         {Kind: filter.DateValue},
	}, fields)
	require.Panics(t, func() { postgres.ExtractFilterFields[model]("not_a_column") })
}
