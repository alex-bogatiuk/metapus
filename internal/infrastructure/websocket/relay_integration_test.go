//go:build integration

package websocket

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// The worker's NotifyBroadcaster and the server's RunRelay meet only in
// PostgreSQL: an event published on one pool reaches a hub listening on
// another connection.
func TestRelayDeliversPublishedEventsAcrossConnections(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_DATABASE_DSN is required")
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pool.Close()

	hub := &Hub{conns: make(map[string]map[string]map[*Connection]struct{})}
	conn := &Connection{TenantID: "tenant-1", UserID: "user-1", Send: make(chan []byte, 8)}
	hub.Register(conn)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); RunRelay(ctx, pool.Config().ConnConfig, hub) }()
	defer func() { cancel(); <-done }()

	broadcaster := NewNotifyBroadcaster(pool)
	// LISTEN starts asynchronously: publish until the relay hears one.
	require.Eventually(t, func() bool {
		broadcaster.BroadcastToUser("tenant-1", "user-1", "notification", map[string]any{"title": "ok"})
		select {
		case msg := <-conn.Send:
			return len(msg) > 0
		case <-time.After(100 * time.Millisecond):
			return false
		}
	}, 10*time.Second, 50*time.Millisecond)
}
