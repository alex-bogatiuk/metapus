// Package watcherlease owns the independent renewal lifetime of a watcher.
package watcherlease

import (
	"context"
	"time"
)

// Start renews while RPC or inbox capture is blocked. Stop cancels and joins
// the owned goroutine; a renewal failure cancels all watcher work with its cause.
// renew must close over an immutable lease identity, never a moving cursor.
func Start(parent context.Context, interval, timeout time.Duration, renew func(context.Context) error) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				attempt, stop := context.WithTimeout(ctx, timeout)
				err := renew(attempt)
				stop()
				if err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(nil); <-done }
}
