package compiler_test

import (
	"math"
	"net/http"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain/filter"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/reporting/compiler"
	"github.com/stretchr/testify/require"
)

func TestReportsResolveExactFiltersFromDatasetMetadata(t *testing.T) {
	c, dataset := fixtureCompiler(t)
	// The fixture amount is int64 minor units (no raw amount type), so exact
	// values up to the int64 bounds must survive without float rounding.
	ctx := fixturePrincipal(dataset)
	for _, tc := range []struct {
		dataset, field string
		value          any
		want           int64
	}{
		{dataset, "amount", "9007199254740993", 9007199254740993},
		{dataset, "amount", "-9223372036854775808", math.MinInt64},
	} {
		q, err := c.BuildQuery(ctx, compiler.QueryRequest{Dataset: tc.dataset, AdvancedFilters: []filter.Item{{Field: tc.field, FieldType: "date", Scale: math.MaxInt, Operator: filter.GreaterOrEqual, Value: tc.value}}})
		require.NoError(t, err)
		sql, args, err := q.ToSql()
		require.NoError(t, err)
		require.Contains(t, sql, "base."+tc.field+" >= $")
		require.Contains(t, args, tc.want)
		require.NotContains(t, sql, "DATE(base."+tc.field+")")
	}
	for _, tc := range []struct {
		dataset, field string
		value          any
	}{
		{dataset, "amount", json.Number("123")},
		{dataset, "amount", "9223372036854775808"},
	} {
		// Execute deliberately has no database. Invalid input must fail before SQL.
		_, err := c.Execute(ctx, compiler.QueryRequest{Dataset: tc.dataset, AdvancedFilters: []filter.Item{{Field: tc.field, Operator: filter.Equal, Value: tc.value}}})
		require.Equal(t, http.StatusBadRequest, apperror.GetHTTPStatus(err), "%s: %v", tc.field, err)
	}
}
