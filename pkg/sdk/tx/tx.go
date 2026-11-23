// Package tx runs work in database transactions and after their commit.
package tx

import (
	"context"

	core "github.com/alex-bogatiuk/metapus/internal/core/tx"
)

type (
	// Manager defines the contract for transaction management.
	// Implementations handle BEGIN, COMMIT, ROLLBACK, and nested transaction support.
	//
	// Domain services depend on this interface, not concrete implementations.
	// The actual implementation lives in infrastructure/storage/postgres.
	Manager = core.Manager
)

// OnCommit schedules a local callback after the outermost successful commit.
// Registration outside a live transaction fails instead of running too early.
func OnCommit(ctx context.Context, fn func(context.Context)) error { return core.OnCommit(ctx, fn) }

// WithCommitScope is used by transaction adapters for a transaction/savepoint.
func WithCommitScope(ctx context.Context) (context.Context, *core.CommitScope) {
	return core.WithCommitScope(ctx)
}
