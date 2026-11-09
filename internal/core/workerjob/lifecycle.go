package workerjob

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

var ErrLeaseLost = errors.New("background task lease lost")

// Supervise restarts a component after an unexpected exit. Each attempt owns
// its resources and must drain its children before returning.
func Supervise(ctx context.Context, minimum, maximum time.Duration, run func(context.Context) error, onExit func(error, time.Duration)) {
	delay := minimum
	for ctx.Err() == nil {
		started := time.Now()
		err := run(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("background component exited unexpectedly")
		}
		if time.Since(started) >= maximum {
			delay = minimum
		}
		wait := delay + time.Duration(rand.Int64N(max(1, int64(delay/4))))
		if onExit != nil {
			onExit(err, wait)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(maximum, delay*2)
	}
}

// WithLease joins its renewal goroutine before returning. Losing the lease
// cancels the handler; callers must fence acknowledgement with the claim token.
func WithLease(ctx context.Context, interval time.Duration, renew func(context.Context) error, work func(context.Context) error) error {
	workCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(context.Canceled)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-workCtx.Done():
				return
			case <-ticker.C:
				renewCtx, renewCancel := context.WithTimeout(workCtx, min(interval, 5*time.Second))
				err := renew(renewCtx)
				renewCancel()
				if err != nil {
					cancel(errors.Join(ErrLeaseLost, err))
					return
				}
			}
		}
	}()
	err := work(workCtx)
	close(stop)
	<-done
	return errors.Join(err, context.Cause(workCtx))
}
