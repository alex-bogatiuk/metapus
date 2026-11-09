package postgres

import (
	"context"
	"regexp"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/apperror"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/listview"
	"metapus/internal/domain/reports/variants"
)

// Capture the executed statement at the pgx boundary. A stale write must carry
// an expected-version predicate and translate PostgreSQL's zero rows to 409.
type versionCapturingTx struct {
	pgx.Tx
	query string
	args  []any
}

func (tx *versionCapturingTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.query, tx.args = query, args
	return noMatchingVersionRow{}
}

type noMatchingVersionRow struct{}

func (noMatchingVersionRow) Scan(...any) error { return pgx.ErrNoRows }

func TestSavedConfigurationUpdatesEnforceExpectedVersionInSQL(t *testing.T) {
	for _, test := range []struct {
		name   string
		update func(context.Context) error
	}{
		{"report variant", func(ctx context.Context) error {
			return NewReportVariantRepo().Update(ctx, &variants.ReportVariant{ID: uuid.New(), Version: 7})
		}},
		{"list view", func(ctx context.Context) error {
			return NewListViewRepo().Update(ctx, &listview.ListView{ID: uuid.New(), Version: 7})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			capture := &versionCapturingTx{}
			ctx := tenant.WithTxManager(context.Background(), &TxManager{})
			ctx = context.WithValue(ctx, txKey{}, &Tx{Tx: capture})
			err := test.update(ctx)
			require.True(t, apperror.IsConcurrentModification(err), "%v", err)
			predicate := regexp.MustCompile(`WHERE .*\bversion = \$(\d+)`).FindStringSubmatch(capture.query)
			require.Len(t, predicate, 2, "the increment alone does not enforce optimistic locking")
			position, err := strconv.Atoi(predicate[1])
			require.NoError(t, err)
			require.Equal(t, 7, capture.args[position-1])
		})
	}
}
