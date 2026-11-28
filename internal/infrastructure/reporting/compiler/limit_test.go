package compiler_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/reporting/compiler"
)

func TestReportQueriesReadOneRowPastTheirLimit(t *testing.T) {
	c, dataset := fixtureCompiler(t)
	ctx := fixturePrincipal(dataset)
	for _, tc := range []struct {
		name  string
		limit int
		want  string
	}{
		{name: "no limit takes the default", limit: 0, want: "LIMIT 5001"},
		{name: "explicit limit", limit: 10, want: "LIMIT 11"},
		{name: "limit above the export ceiling", limit: 1 << 30, want: "LIMIT 100001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, err := c.BuildQuery(ctx, compiler.QueryRequest{Dataset: dataset, Select: []string{"number"}, Limit: tc.limit})
			require.NoError(t, err)
			sql, _, err := q.ToSql()
			require.NoError(t, err)
			require.Contains(t, sql, tc.want)
		})
	}
}

func TestTruncatedReportIsRefusedWhereAllRowsCount(t *testing.T) {
	complete := &compiler.QueryResult{Items: make([]map[string]any, 3), TotalItems: 3}
	require.NoError(t, complete.RequireComplete())

	cut := &compiler.QueryResult{Items: make([]map[string]any, 3), TotalItems: 3, Truncated: true}
	err := cut.RequireComplete()
	require.Error(t, err)
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, apperror.CodeValidation, appErr.Code)
}
