package command

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type testTxManager struct{}

func (testTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type testSavepointManager struct {
	savepoints int
}

func (m *testSavepointManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (m *testSavepointManager) RunInSavepoint(ctx context.Context, fn func(context.Context) error) error {
	m.savepoints++
	return fn(ctx)
}

type testRepository struct {
	record   Record
	exists   bool
	complete int
}

func (r *testRepository) Acquire(_ context.Context, metadata Metadata) (AcquireResult, error) {
	if r.exists {
		return AcquireResult{Record: r.record}, nil
	}
	r.exists = true
	r.record = Record{Metadata: metadata, Status: "pending"}
	return AcquireResult{Acquired: true}, nil
}

func (r *testRepository) Complete(_ context.Context, metadata Metadata, result Result) error {
	r.complete++
	r.record = Record{Metadata: metadata, Status: "success", Result: result}
	return nil
}

func TestExecutorReplaysCommittedResult(t *testing.T) {
	repo := &testRepository{}
	executor := NewExecutor(testTxManager{}, repo)
	metadata := Metadata{ScopeType: "merchant", ScopeID: "m1", Operation: "invoice.create", IdempotencyKey: "k1", RequestFingerprint: strings.Repeat("a", 64)}
	calls := 0
	callback := func(context.Context) (Result, error) {
		calls++
		return Result{Outcome: "created", ResourceType: "crypto_invoice", ResourceID: "i1", Payload: MustPayload(map[string]string{"id": "i1"})}, nil
	}

	first, err := executor.Execute(context.Background(), metadata, callback)
	if err != nil || first.Replayed {
		t.Fatalf("first execution = %#v, %v", first, err)
	}
	second, err := executor.Execute(context.Background(), metadata, callback)
	if err != nil || !second.Replayed || calls != 1 || repo.complete != 1 {
		t.Fatalf("replay = %#v, err=%v calls=%d complete=%d", second, err, calls, repo.complete)
	}
}

func TestExecutorRejectsFingerprintReuse(t *testing.T) {
	repo := &testRepository{}
	executor := NewExecutor(testTxManager{}, repo)
	metadata := Metadata{ScopeType: "merchant", ScopeID: "m1", Operation: "invoice.create", IdempotencyKey: "k1", RequestFingerprint: strings.Repeat("a", 64)}
	_, err := executor.Execute(context.Background(), metadata, func(context.Context) (Result, error) { return Result{Outcome: "created"}, nil })
	if err != nil {
		t.Fatal(err)
	}
	metadata.RequestFingerprint = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	_, err = executor.Execute(context.Background(), metadata, func(context.Context) (Result, error) { return Result{Outcome: "created"}, nil })
	if !errors.Is(err, ErrKeyReused) {
		t.Fatalf("expected ErrKeyReused, got %v", err)
	}
}

func TestExecutorPersistsMappedFailureAfterSavepointRollback(t *testing.T) {
	repo := &testRepository{}
	txm := &testSavepointManager{}
	executor := NewExecutor(txm, repo)
	metadata := Metadata{ScopeType: "merchant", ScopeID: "m1", Operation: "invoice.create", IdempotencyKey: "k1", RequestFingerprint: strings.Repeat("a", 64)}
	calls := 0
	businessErr := errors.New("stable business rejection")
	mapper := func(err error) (Result, bool) {
		if !errors.Is(err, businessErr) {
			return Result{}, false
		}
		return Result{Outcome: "error:422", Payload: MustPayload(map[string]string{"code": "LIMIT"})}, true
	}

	first, err := executor.ExecuteWithFailureMapper(context.Background(), metadata, func(context.Context) (Result, error) {
		calls++
		return Result{}, businessErr
	}, mapper)
	if err != nil || first.Replayed || first.Result.Outcome != "error:422" {
		t.Fatalf("first failure = %#v, %v", first, err)
	}
	second, err := executor.ExecuteWithFailureMapper(context.Background(), metadata, func(context.Context) (Result, error) {
		calls++
		return Result{}, businessErr
	}, mapper)
	if err != nil || !second.Replayed || calls != 1 || repo.complete != 1 || txm.savepoints != 1 {
		t.Fatalf("failure replay = %#v, err=%v calls=%d complete=%d savepoints=%d", second, err, calls, repo.complete, txm.savepoints)
	}
}

func TestExecutorDoesNotPersistUnmappedFailure(t *testing.T) {
	repo := &testRepository{}
	txm := &testSavepointManager{}
	executor := NewExecutor(txm, repo)
	metadata := Metadata{ScopeType: "merchant", ScopeID: "m1", Operation: "invoice.create", IdempotencyKey: "k1", RequestFingerprint: strings.Repeat("a", 64)}
	failure := errors.New("transient")

	_, err := executor.ExecuteWithFailureMapper(context.Background(), metadata, func(context.Context) (Result, error) {
		return Result{}, failure
	}, func(error) (Result, bool) { return Result{}, false })
	if !errors.Is(err, failure) || repo.complete != 0 {
		t.Fatalf("unmapped failure err=%v complete=%d", err, repo.complete)
	}
}

func TestFingerprintIgnoresMapFieldOrder(t *testing.T) {
	a, err := Fingerprint(map[string]any{"amount": "10", "network": "tron"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Fingerprint(map[string]any{"network": "tron", "amount": "10"})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("fingerprints differ: %s != %s", a, b)
	}
}
