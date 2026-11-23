// Package workerjob supervises leased background jobs and records their
// runs.
package workerjob

import (
	"context"
	"time"

	core "github.com/alex-bogatiuk/metapus/internal/core/workerjob"
)

type (
	// Recorder wraps background task functions and persists execution records.
	// It is best-effort: repository errors are logged, never propagated.
	Recorder = core.Recorder
)

var (
	ErrLeaseLost = core.ErrLeaseLost
)

// Supervise restarts a component after an unexpected exit. Each attempt owns
// its resources and must drain its children before returning.
func Supervise(ctx context.Context, minimum time.Duration, maximum time.Duration, run func(context.Context) error, onExit func(error, time.Duration)) {
	core.Supervise(ctx, minimum, maximum, run, onExit)
}

// WithLease joins its renewal goroutine before returning. Losing the lease
// cancels the handler; callers must fence acknowledgement with the claim token.
func WithLease(ctx context.Context, interval time.Duration, renew func(context.Context) error, work func(context.Context) error) error {
	return core.WithLease(ctx, interval, renew, work)
}
