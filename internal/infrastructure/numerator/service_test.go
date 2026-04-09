package numerator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	corenumerator "metapus/internal/core/numerator"
)

// Mock objects
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

type mockQuerier struct {
	mu           sync.Mutex
	currentValue int64 // Simulates DB sequence value
	lastIncr     int64 // Track last increment passed
}

func (m *mockQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	m.mu.Lock()
	defer m.mu.Unlock()

	var increment int64 = 1
	if len(args) == 2 {
		if val, ok := args[1].(int64); ok {
			increment = val
		}
	}

	m.currentValue += increment
	m.lastIncr = increment

	return &mockRow{val: m.currentValue}
}

// newTestService creates a Service with a mock querier injected via querierFn.
func newTestService(q Querier) *Service {
	svc := New()
	svc.querierFn = func(ctx context.Context) Querier { return q }
	return svc
}

func TestGetNextNumber_Strict(t *testing.T) {
	q := &mockQuerier{}
	svc := newTestService(q)
	ctx := context.Background()
	cfg := corenumerator.DefaultConfig("TEST")

	// 1. First call
	num, err := svc.GetNextNumber(ctx, cfg, nil, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != "TEST-2026-00001" { // mock starts at 1
		t.Errorf("expected TEST-2026-00001, got %s", num)
	}

	// 2. Second call
	num, err = svc.GetNextNumber(ctx, cfg, nil, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != "TEST-2026-00002" {
		t.Errorf("expected TEST-2026-00002, got %s", num)
	}
}

func TestGetNextNumber_Cached(t *testing.T) {
	q := &mockQuerier{}
	svc := newTestService(q)
	ctx := context.Background()
	cfg := corenumerator.DefaultConfig("ORD")

	opts := &corenumerator.Options{
		Strategy:  corenumerator.StrategyCached,
		RangeSize: 10,
	}

	// 1. First call - should trigger DB fetch (allocate 1..10)
	num, err := svc.GetNextNumber(ctx, cfg, opts, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != "ORD-2026-00001" {
		t.Errorf("expected ORD-2026-00001, got %s", num)
	}

	// Check DB calls
	if q.currentValue != 10 {
		t.Errorf("expected DB value to be 10, got %d", q.currentValue)
	}

	// 2. Second call - should be from memory
	num, err = svc.GetNextNumber(ctx, cfg, opts, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != "ORD-2026-00002" {
		t.Errorf("expected ORD-2026-00002, got %s", num)
	}
	if q.currentValue != 10 {
		t.Errorf("expected DB value to stay 10, got %d", q.currentValue)
	}

	// 3. Exhaust range
	for i := 0; i < 8; i++ {
		_, _ = svc.GetNextNumber(ctx, cfg, opts, time.Now())
	}

	// Next call should trigger DB again (allocate 11..20)
	num, err = svc.GetNextNumber(ctx, cfg, opts, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != "ORD-2026-00011" {
		t.Errorf("expected ORD-2026-00011, got %s", num)
	}
	if q.currentValue != 20 {
		t.Errorf("expected DB value to be 20, got %d", q.currentValue)
	}
}

func TestSetNextNumber_InvalidatesCache(t *testing.T) {
	q := &mockQuerier{}
	svc := newTestService(q)
	ctx := context.Background()
	cfg := corenumerator.DefaultConfig("INV")
	opts := &corenumerator.Options{Strategy: corenumerator.StrategyCached, RangeSize: 10}
	now := time.Now()

	// 1. Fill cache (1..10)
	_, _ = svc.GetNextNumber(ctx, cfg, opts, now)

	// 2. Verify cache entry exists (via sharded lock)
	key := svc.buildKey(cfg, now)
	sh := svc.getShard(key)
	sh.mu.Lock()
	_, hasBefore := sh.ranges[key]
	sh.mu.Unlock()
	if !hasBefore {
		t.Fatal("expected cache entry to exist after GetNextNumber")
	}

	// 3. SetNextNumber should invalidate cache
	_ = svc.SetNextNumber(ctx, cfg, now, 100)

	sh.mu.Lock()
	_, hasAfter := sh.ranges[key]
	sh.mu.Unlock()
	if hasAfter {
		t.Error("expected cache entry to be invalidated after SetNextNumber")
	}
}
