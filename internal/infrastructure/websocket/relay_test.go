package websocket

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
)

func TestRelayEventReachesTheUsersConnections(t *testing.T) {
	hub := &Hub{conns: make(map[string]map[string]map[*Connection]struct{})}
	conn := &Connection{TenantID: "tenant-1", UserID: "user-1", Send: make(chan []byte, 1)}
	hub.Register(conn)

	body, err := encodeRelayEvent("tenant-1", "user-1", "notification", map[string]any{"title": "Готово"})
	require.NoError(t, err)
	deliverRelayEvent(context.Background(), hub, string(body))

	var msg struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(<-conn.Send, &msg))
	require.Equal(t, "notification", msg.Type)
	require.Equal(t, map[string]any{"title": "Готово"}, msg.Payload)
}

func TestRelayEventOverTheNotifyLimitIsRejected(t *testing.T) {
	_, err := encodeRelayEvent("tenant-1", "user-1", "notification", map[string]any{"message": strings.Repeat("x", maxRelayPayload)})
	require.ErrorIs(t, err, errRelayEventTooLarge)
}

func TestMalformedRelayEventIsIgnored(t *testing.T) {
	hub := &Hub{conns: make(map[string]map[string]map[*Connection]struct{})}
	conn := &Connection{TenantID: "tenant-1", UserID: "user-1", Send: make(chan []byte, 1)}
	hub.Register(conn)

	deliverRelayEvent(context.Background(), hub, "{not json")
	require.Empty(t, conn.Send)
}
