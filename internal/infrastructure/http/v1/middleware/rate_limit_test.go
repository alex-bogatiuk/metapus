package middleware

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRateLimiterRefillsAndEvictsWithoutBackgroundLifetime(t *testing.T) {
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	limiter := NewRateLimiter(1, 2)
	limiter.now = func() time.Time { return now }
	require.True(t, limiter.allow("one"))
	require.True(t, limiter.allow("one"))
	require.False(t, limiter.allow("one"))
	now = now.Add(time.Second)
	require.True(t, limiter.allow("one"))
	now = now.Add(11 * time.Minute)
	require.True(t, limiter.allow("two"))
	require.NotContains(t, limiter.buckets, "one")
}
