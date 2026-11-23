package readaccess_test

import (
	"context"
	"errors"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/application/readaccess"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	"github.com/stretchr/testify/require"
)

type currencyRegistry struct{}

func (currencyRegistry) Definition(key string) (readaccess.Definition, bool) {
	return readaccess.Definition{Key: "currency", Permission: "currency:read", Fields: []readaccess.Field{{Name: "isoCode", Column: "iso_code"}, {Name: "decimalPlaces", Column: "decimal_places"}}}, key == "currency"
}

func TestMoneyProjectionChecksRelatedPolicyAndBatchesUnits(t *testing.T) {
	currencyID, documentA, documentB := id.New(), id.New(), id.New()
	rows := []readaccess.Projection{
		{ID: documentA, Fields: map[string]any{"totalAmount": types.MinorUnits(9007199254740993), "currencyId": currencyID}},
		{ID: documentB, Fields: map[string]any{"totalAmount": types.MinorUnits(-9223372036854775808), "currencyId": currencyID}},
	}
	fields := readaccess.MoneyFields{Amount: "totalAmount", CurrencyID: "currencyId", CurrencyEntity: "currency"}
	base := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: id.New().String(), Permissions: []string{"currency:read"}})
	for _, scale := range []int{0, 2, 3, 9} {
		calls := 0
		repo := repository{read: func(query readaccess.Query) ([]readaccess.Record, error) {
			calls++
			require.Equal(t, []id.ID{currencyID}, query.IDs)
			return []readaccess.Record{{ID: currencyID, Fields: map[string]any{"isoCode": "USD", "decimalPlaces": scale}, Dimensions: map[string]string{"organization": "A"}}}, nil
		}}
		service := readaccess.New(currencyRegistry{}, repo, nil)
		actual, err := service.ReadMoney(base, rows, fields)
		require.NoError(t, err)
		require.Len(t, actual, 2)
		require.Equal(t, 1, calls)
		require.Equal(t, rows[0].Fields["totalAmount"], actual[documentA].Amount)
		require.Equal(t, rows[1].Fields["totalAmount"], actual[documentB].Amount)
		require.Equal(t, readaccess.CurrencyUnit{ID: currencyID, Code: "USD", DecimalPlaces: scale}, actual[documentA].Unit)
		for _, hidden := range []string{"iso_code", "decimal_places"} {
			ctx := security.WithFieldPolicies(base, map[string]*security.FieldPolicy{"currency:read": {AllowedFields: []string{"*", "-" + hidden}}})
			actual, err := service.ReadMoney(ctx, rows, fields)
			require.NoError(t, err)
			require.Empty(t, actual)
		}
		ctx := security.WithDataScope(base, &security.DataScope{Dimensions: map[string][]string{"organization": {}}})
		actual, err = service.ReadMoney(ctx, rows, fields)
		require.NoError(t, err)
		require.Empty(t, actual)
	}
	never := readaccess.New(currencyRegistry{}, repository{read: func(readaccess.Query) ([]readaccess.Record, error) { t.Fatal("must not query units"); return nil, nil }}, nil)
	result, err := never.ReadMoney(readerContext(), rows, fields) // no currency permission
	require.NoError(t, err)
	require.Empty(t, result)
	result, err = never.ReadMoney(base, []readaccess.Projection{{ID: documentA, Fields: map[string]any{"currencyId": currencyID}}}, fields)
	require.NoError(t, err)
	require.Empty(t, result)
	failure := errors.New("storage unavailable")
	failed := readaccess.New(currencyRegistry{}, repository{read: func(readaccess.Query) ([]readaccess.Record, error) { return nil, failure }}, nil)
	_, err = failed.ReadMoney(base, rows, fields)
	require.ErrorIs(t, err, failure)
}
