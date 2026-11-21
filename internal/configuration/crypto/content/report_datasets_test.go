package content

import (
	"context"
	"errors"
	"github.com/Masterminds/squirrel"
	"github.com/stretchr/testify/require"
	"metapus/internal/infrastructure/readpolicy"
	"testing"
)

type brokenReportPredicate struct{ err error }

func (p brokenReportPredicate) ToSql() (string, []any, error) { return "", nil, p.err }

func TestReportNestedSQLFailuresPropagate(t *testing.T) {
	failure := errors.New("injected nested SQL failure")
	for _, dataset := range AllDatasets() {
		t.Run(dataset.Key, func(t *testing.T) {
			plan := readpolicy.ReadConstraints{Sources: map[string]squirrel.SelectBuilder{}}
			for _, source := range dataset.Sources {
				plan.Sources[source.Key] = squirrel.Select().From("test_source " + source.Alias).Where(brokenReportPredicate{failure})
			}
			q, err := DatasetExecutors()[dataset.ExecutorKey].BuildQuery(context.Background(), map[string]any{"from_date": "2026-01-01", "to_date": "2026-02-01"}, plan)
			require.NoError(t, err)
			_, _, err = q.Column("base.*").PlaceholderFormat(squirrel.Dollar).ToSql()
			require.ErrorIs(t, err, failure)
		})
	}
}
