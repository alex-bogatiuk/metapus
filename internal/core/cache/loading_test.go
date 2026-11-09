package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadingBoundsAndExpiresAllOldKeys(t *testing.T) {
	c := NewLoading[int, int](time.Minute, 2)
	now := time.Now()
	c.now = func() time.Time { return now }
	get := func(key int) int {
		v, err := c.Get(context.Background(), key, func(context.Context) (int, error) { return key, nil })
		require.NoError(t, err)
		return v
	}
	for key := range 3 {
		require.Equal(t, key, get(key))
	}
	require.Len(t, c.entries, 2)
	require.NotContains(t, c.entries, 0)
	now = now.Add(time.Minute)
	require.Equal(t, 4, get(4))
	require.Len(t, c.entries, 1)
	require.Equal(t, 1, c.order.Len())
}

func TestLoadingCoalescesConcurrentMisses(t *testing.T) {
	c := NewLoading[string, int](time.Minute, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	loader := func(ctx context.Context) (int, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
			return 42, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	results := make(chan int, 16)
	errors := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { v, err := c.Get(ctx, "key", loader); results <- v; errors <- err })
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("loader did not start")
	}
	close(release)
	wg.Wait()
	for range 16 {
		require.NoError(t, <-errors)
		require.Equal(t, 42, <-results)
	}
	require.EqualValues(t, 1, calls.Load())
}

func TestLoadingInvalidationFencesAnInFlightValue(t *testing.T) {
	c := NewLoading[string, int](time.Minute, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	type result struct {
		value int
		err   error
	}
	finished := make(chan result, 1)
	var calls atomic.Int32
	loader := func(ctx context.Context) (int, error) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
				return 1, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		return 2, nil
	}
	go func() { v, err := c.Get(ctx, "user", loader); finished <- result{v, err} }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("loader did not start")
	}
	c.Invalidate(func(key string) bool { return key == "user" })
	// A request after invalidation must not join the blocked old load.
	v, err := c.Get(ctx, "user", loader)
	require.NoError(t, err)
	require.Equal(t, 2, v)
	close(release)
	select {
	case r := <-finished:
		require.NoError(t, r.err)
		require.Equal(t, 2, r.value)
	case <-ctx.Done():
		t.Fatal("invalidated request did not finish")
	}
	require.EqualValues(t, 2, calls.Load())
}

func TestLoadingWaiterCanCancelWithoutCancellingLeader(t *testing.T) {
	c := NewLoading[string, int](time.Minute, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := c.Get(ctx, "key", func(ctx context.Context) (int, error) {
			close(entered)
			select {
			case <-release:
				return 42, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		})
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("loader did not start")
	}
	waitCtx, stop := context.WithTimeout(ctx, 10*time.Millisecond)
	defer stop()
	_, err := c.Get(waitCtx, "key", func(context.Context) (int, error) { t.Error("waiter must not load"); return 0, nil })
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(release)
	require.NoError(t, <-done)
}

func TestLoadingDoesNotCacheErrorsAndRecoversAfterLoaderPanic(t *testing.T) {
	c := NewLoading[string, int](time.Minute, 2)
	sentinel := errors.New("unavailable")
	_, err := c.Get(context.Background(), "key", func(context.Context) (int, error) { return 0, sentinel })
	require.ErrorIs(t, err, sentinel)
	require.Panics(t, func() {
		_, _ = c.Get(context.Background(), "key", func(context.Context) (int, error) { panic("loader") })
	})
	require.Empty(t, c.loads)
	v, err := c.Get(context.Background(), "key", func(context.Context) (int, error) { return 7, nil })
	require.NoError(t, err)
	require.Equal(t, 7, v)
}

// Observing Done lets the test cancel the leader only once another request is
// waiting, without relying on scheduler timing or sleeps.
type observedContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func (c *observedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestLoadingWaiterRetriesAfterLeaderCancellation(t *testing.T) {
	c := NewLoading[string, int](time.Minute, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	leaderCtx, stopLeader := context.WithCancel(ctx)
	defer stopLeader()
	entered, leaderDone := make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := c.Get(leaderCtx, "key", func(ctx context.Context) (int, error) {
			close(entered)
			<-ctx.Done()
			return 0, ctx.Err()
		})
		leaderDone <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("leader did not start")
	}
	waitCtx := &observedContext{Context: ctx, waiting: make(chan struct{})}
	result := make(chan int, 1)
	errors := make(chan error, 1)
	go func() {
		v, err := c.Get(waitCtx, "key", func(context.Context) (int, error) { return 42, nil })
		result <- v
		errors <- err
	}()
	select {
	case <-waitCtx.waiting:
	case <-ctx.Done():
		t.Fatal("second request did not wait")
	}
	stopLeader()
	require.ErrorIs(t, <-leaderDone, context.Canceled)
	require.NoError(t, <-errors)
	require.Equal(t, 42, <-result)
}
