package compiler_test

import (
	"context"
	"math"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/json"
	"metapus/internal/domain/filter"
	"metapus/internal/domain/reports/schema"
	"metapus/internal/infrastructure/reporting/compiler"
)

func TestReportsResolveExactFiltersFromDatasetMetadata(t *testing.T) {
	c := reportCompiler(t)
	// Exercise both wire contracts against the same trusted numeric projection.
	// The concrete journal now stores crypto raw units; ERP still uses int64.
	c.GetDataset("document-journal").Fields = append([]schema.Field(nil), c.GetDataset("document-journal").Fields...)
	amountField := c.GetDataset("document-journal").FindField("total_amount")
	unit := *amountField.Money
	unit.AmountType = ""
	amountField.Money = &unit
	principal := reportPrincipal()
	ctx := appctx.WithUser(context.Background(), &principal)
	for _, tc := range []struct {
		dataset, field string
		value          any
		want           int64
	}{
		{"document-journal", "total_amount", "9007199254740993", 9007199254740993},
		{"document-journal", "total_amount", "-9223372036854775808", math.MinInt64},
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
		{"document-journal", "total_amount", json.Number("123")},
		{"document-journal", "total_amount", "9223372036854775808"},
	} {
		// Execute deliberately has no database. Invalid input must fail before SQL.
		_, err := c.Execute(ctx, compiler.QueryRequest{Dataset: tc.dataset, AdvancedFilters: []filter.Item{{Field: tc.field, Operator: filter.Equal, Value: tc.value}}})
		require.Equal(t, http.StatusBadRequest, apperror.GetHTTPStatus(err), "%s: %v", tc.field, err)
	}
}
