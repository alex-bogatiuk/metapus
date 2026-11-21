package outbound_repo

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type providerBalanceRow struct {
	value string
}

func (r providerBalanceRow) Scan(dest ...any) error {
	receiver, ok := dest[0].(sql.Scanner)
	if !ok {
		return nil
	}
	return receiver.Scan(r.value)
}

type providerBalanceExec struct {
	query string
	args  []any
}

type providerBalanceQuerierStub struct {
	ledger string
	execs  []providerBalanceExec
}

func (q *providerBalanceQuerierStub) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, "FOR UPDATE") {
		return providerBalanceRow{value: args[0].(id.ID).String()}
	}
	return providerBalanceRow{value: q.ledger}
}

func (q *providerBalanceQuerierStub) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	q.execs = append(q.execs, providerBalanceExec{query: query, args: args})
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestRecordProviderBalanceObservationAcceptsNegativePreOpeningLedger(t *testing.T) {
	q := &providerBalanceQuerierStub{ledger: "-650000"}
	providerAmount := types.NewRawAmountFromInt64(99_350_000)

	err := recordProviderBalanceObservation(
		context.Background(), q, id.New(), id.New(), "rental-1", providerAmount,
	)
	require.NoError(t, err)
	require.Len(t, q.execs, 2)

	ledger, ok := q.execs[0].args[3].(types.RawDelta)
	require.True(t, ok)
	require.Equal(t, "-650000", ledger.String())
	delta, ok := q.execs[0].args[5].(types.RawDelta)
	require.True(t, ok)
	require.Equal(t, "100000000", delta.String())
}
