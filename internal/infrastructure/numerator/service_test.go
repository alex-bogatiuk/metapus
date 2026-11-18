package numerator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	corenumerator "metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
)

type mockRow struct {
	val int64
	err error
}

func (m *mockRow) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	if len(dest) > 0 {
		if ptr, ok := dest[0].(*int64); ok {
			*ptr = m.val
		}
	}
	return nil
}

// mockQuerier simulates sys_sequences: one counter per key.
type mockQuerier struct {
	mu       sync.Mutex
	counters map[string]int64
	calls    int
}

func (m *mockQuerier) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counters == nil {
		m.counters = map[string]int64{}
	}
	m.calls++
	key := args[0].(string)
	if len(args) == 2 {
		m.counters[key] = args[1].(int64)
	} else {
		m.counters[key]++
	}
	return &mockRow{val: m.counters[key]}
}

type transactionQuerier struct {
	*mockQuerier
}

func (q *transactionQuerier) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// newTestService creates a Service with a mock querier injected via querierFn.
func newTestService(q Querier) *Service {
	svc := New()
	svc.querierFn = func(ctx context.Context) Querier { return q }
	return svc
}

var december = time.Date(2025, 12, 30, 10, 0, 0, 0, time.UTC)

func TestGetNextNumberIncrementsTheSeriesOfThePeriod(t *testing.T) {
	q := &mockQuerier{}
	svc := newTestService(q)
	ctx := context.Background()
	cfg := corenumerator.DefaultConfig("TEST")

	for _, want := range []string{"TEST-2025-00001", "TEST-2025-00002"} {
		num, err := svc.GetNextNumber(ctx, cfg, december)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if num != want {
			t.Errorf("got %s, want %s", num, want)
		}
	}
	num, err := svc.GetNextNumber(ctx, cfg, december.AddDate(0, 0, 3))
	if err != nil {
		t.Fatal(err)
	}
	if num != "TEST-2026-00001" {
		t.Errorf("a new year starts its own counter, got %s", num)
	}
}

func TestGetNextNumberKeysACatalogSeriesByItsKey(t *testing.T) {
	q := &mockQuerier{}
	svc := newTestService(q)
	series := corenumerator.Config{Key: "catalog.organization", Prefix: "ORG", PadWidth: 6, ResetPeriod: "never"}
	if num, _ := svc.GetNextNumber(context.Background(), series, time.Time{}); num != "ORG-000001" {
		t.Fatalf("got %s", num)
	}
	series.Prefix = "ОРГ"
	if num, _ := svc.GetNextNumber(context.Background(), series, time.Time{}); num != "ОРГ-000002" {
		t.Fatalf("a new prefix continues the counter, got %s", num)
	}
	if q.counters["catalog.organization"] != 2 {
		t.Fatalf("counters %v", q.counters)
	}
}

func TestGetNextNumberPrefersTransactionAwareQuerier(t *testing.T) {
	txQuerier := &transactionQuerier{mockQuerier: &mockQuerier{}}
	ctx := tenant.WithTxManager(context.Background(), txQuerier)

	number, err := New().GetNextNumber(ctx, corenumerator.DefaultConfig("TX"), december)
	if err != nil {
		t.Fatalf("GetNextNumber() error = %v", err)
	}
	if number != "TX-2025-00001" {
		t.Fatalf("number = %q, want TX-2025-00001", number)
	}
	if txQuerier.calls != 1 {
		t.Fatalf("transaction querier calls = %d, want 1", txQuerier.calls)
	}
}

func TestSetNextNumberMovesTheCounter(t *testing.T) {
	q := &mockQuerier{}
	svc := newTestService(q)
	cfg := corenumerator.DefaultConfig("INV")
	if err := svc.SetNextNumber(context.Background(), cfg, december, 100); err != nil {
		t.Fatal(err)
	}
	if num, _ := svc.GetNextNumber(context.Background(), cfg, december); num != "INV-2025-00101" {
		t.Fatalf("got %s", num)
	}
}
