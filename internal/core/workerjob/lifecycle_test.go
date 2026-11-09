package workerjob

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSuperviseRestartsAfterDependencyRecoversAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	ready, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		Supervise(ctx, time.Millisecond, 10*time.Millisecond, func(ctx context.Context) error {
			if calls.Add(1) == 1 {
				return errors.New("temporary database outage")
			}
			close(ready)
			<-ctx.Done()
			return ctx.Err()
		}, nil)
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("component did not restart")
	}
	cancel()
	<-done
	require.EqualValues(t, 2, calls.Load())
}
func TestWithLeaseCancelsHandlerAfterOwnershipLossAndJoinsRenewal(t *testing.T) {
	var renewals atomic.Int32
	err := WithLease(context.Background(), time.Millisecond, func(context.Context) error { renewals.Add(1); return ErrLeaseLost }, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	require.ErrorIs(t, err, ErrLeaseLost)
	require.EqualValues(t, 1, renewals.Load())
}
func TestWithLeaseDoesNotRenewAfterWorkCompletes(t *testing.T) {
	var renewals atomic.Int32
	require.NoError(t, WithLease(context.Background(), time.Hour, func(context.Context) error { renewals.Add(1); return nil }, func(context.Context) error { return nil }))
	require.Zero(t, renewals.Load())
}
