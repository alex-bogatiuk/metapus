// Package tx provides transaction management abstractions.
// This package defines interfaces that decouple domain logic from specific
// database implementations, following the Dependency Inversion Principle.
package tx

import (
	"context"
)

// Manager defines the contract for transaction management.
// Implementations handle BEGIN, COMMIT, ROLLBACK, and nested transaction support.
//
// Domain services depend on this interface, not concrete implementations.
// The actual implementation lives in infrastructure/storage/postgres.
type Manager interface {
	// RunInTransaction executes fn within a database transaction.
	// If fn returns an error, the transaction is rolled back.
	// If fn succeeds, the transaction is committed.
	//
	// Nested calls reuse the existing transaction from context.
	RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// ReadOnlyManager extends Manager with read-only transaction support.
// Use for queries that don't modify data (better performance, no locks).
type ReadOnlyManager interface {
	Manager

	// ReadOnly executes fn in a read-only transaction.
	// Attempts to modify data will fail.
	ReadOnly(ctx context.Context, fn func(ctx context.Context) error) error
}

