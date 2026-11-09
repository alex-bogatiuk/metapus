package rpcpolicy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSharedAdmissionReservesUrgentCapacityAndHonorsCooldown(t *testing.T) {
	capacity := Capacity{Concurrency: 3, BulkConcurrency: 2, RequestsPerSecond: 1000}
	a, err := Acquire(t.Name(), capacity)
	require.NoError(t, err)
	defer Release(a)
	b, err := Acquire(t.Name(), capacity)
	require.NoError(t, err)
	defer Release(b)
	require.Same(t, a, b, "tenants and roles share endpoint quota")
	_, err = Acquire(t.Name(), Capacity{Concurrency: 4, BulkConcurrency: 2, RequestsPerSecond: 1000})
	require.Error(t, err)
	one, err := a.Enter(t.Context(), true)
	require.NoError(t, err)
	defer one()
	two, err := b.Enter(t.Context(), true)
	require.NoError(t, err)
	defer two()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = b.Enter(ctx, true)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	urgent, err := b.Enter(t.Context(), false)
	require.NoError(t, err)
	urgent()
	a.Throttle(time.Now().Add(time.Minute))
	ctx2, cancel2 := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel2()
	_, err = b.Enter(ctx2, false)
	require.ErrorIs(t, err, context.DeadlineExceeded, "429 blocks endpoint traffic even on another role")
}

func TestResponseLimitAndRetryAfter(t *testing.T) {
	for _, declared := range []int64{-1, 5} {
		resp := &http.Response{StatusCode: 200, ContentLength: declared, Body: io.NopCloser(strings.NewReader("12345"))}
		err := LimitResponse(resp, 4, nil)
		if declared < 0 {
			require.NoError(t, err)
			_, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}
		require.ErrorIs(t, err, ErrResponseTooLarge)
	}
	now := time.Now()
	for _, value := range []string{"120", now.Add(2 * time.Minute).UTC().Format(http.TimeFormat)} {
		until := RetryAfter(value, now)
		require.Greater(t, until.Sub(now), 119*time.Second)
		require.Less(t, until.Sub(now), 121*time.Second)
	}
}

func TestMinimumRequestIntervalDoesNotBurstAcrossPools(t *testing.T) {
	c := Capacity{Concurrency: 8, BulkConcurrency: 4, RequestsPerSecond: 20, MinRequestIntervalMS: 100}
	a, err := Acquire(t.Name(), c)
	require.NoError(t, err)
	defer Release(a)
	b, err := Acquire(t.Name(), c)
	require.NoError(t, err)
	defer Release(b)
	leave, err := a.Enter(t.Context(), false)
	require.NoError(t, err)
	leave()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = b.Enter(ctx, false)
	require.Error(t, err, "a second pool cannot spend another burst token")
	leave, err = b.Enter(t.Context(), true)
	require.NoError(t, err, "the next time slot remains usable")
	leave()
}

func TestCircuitCancellationAndFailedRecovery(t *testing.T) {
	now := time.Now()
	c := Circuit{Failures: 1, OpenUntil: now.Add(-time.Second)}
	require.True(t, c.Begin(now))
	require.False(t, c.Begin(now), "only one half-open reservation")
	c.Finish(now, time.Millisecond, "timeout", true, 2, time.Minute, time.Time{})
	require.Equal(t, 1, c.Failures)
	require.False(t, c.Probing)
	require.True(t, c.Begin(now))
	c.Finish(now, time.Millisecond, "timeout", false, 5, time.Minute, time.Time{})
	require.True(t, c.OpenUntil.After(now), "failed recovery reopens even below failure threshold")
}
