package tx

import (
	"context"
	"fmt"
)

type commitKey struct{}

// CommitScope contains local callbacks only. Durable external work must be
// published to a transactional outbox, never put into this in-memory queue.
// Like a database transaction, a scope is used by one goroutine at a time.
type CommitScope struct {
	callbacks []func(context.Context)
	closed    bool
}

// WithCommitScope is used by transaction adapters for a transaction/savepoint.
func WithCommitScope(ctx context.Context) (context.Context, *CommitScope) {
	scope := &CommitScope{}
	return context.WithValue(ctx, commitKey{}, scope), scope
}

// OnCommit schedules a local callback after the outermost successful commit.
// Registration outside a live transaction fails instead of running too early.
func OnCommit(ctx context.Context, fn func(context.Context)) error {
	scope, _ := ctx.Value(commitKey{}).(*CommitScope)
	if scope == nil || scope.closed {
		return fmt.Errorf("after-commit hook requires an active transaction")
	}
	scope.callbacks = append(scope.callbacks, fn)
	return nil
}

// Merge transfers callbacks from a released savepoint to its enclosing scope.
func (s *CommitScope) Merge(ctx context.Context) error {
	parent, _ := ctx.Value(commitKey{}).(*CommitScope)
	if parent == nil || parent.closed || s.closed {
		return fmt.Errorf("cannot merge completed transaction hooks")
	}
	parent.callbacks = append(parent.callbacks, s.callbacks...)
	s.Discard()
	return nil
}

func (s *CommitScope) Discard() { s.callbacks = nil; s.closed = true }

// Commit invokes callbacks using a context that no longer holds a database tx.
func (s *CommitScope) Commit(ctx context.Context) {
	callbacks := s.callbacks
	s.Discard()
	for _, callback := range callbacks {
		callback(ctx)
	}
}
