package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWebhookBackoffCoversTenAttemptsAcrossSeventyTwoHours(t *testing.T) {
	backoff := WebhookBackoff()
	require.Equal(t, 30*time.Second, backoff.NextDelay(0))
	require.Equal(t, 90*time.Second, backoff.NextDelay(1))
	require.Equal(t, 8*time.Minute, backoff.NextDelay(2))
	require.Equal(t, 24*time.Hour, backoff.NextDelay(8))
	require.Equal(t, 24*time.Hour, backoff.NextDelay(99))

	var elapsed time.Duration
	for retry := 0; retry < 9; retry++ {
		elapsed += backoff.NextDelay(retry)
	}
	require.Equal(t, 72*time.Hour, elapsed)
}
