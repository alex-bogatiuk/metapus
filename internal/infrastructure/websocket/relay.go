package websocket

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

// The browsers' WebSocket connections live in the API server's hub, while
// automations run in the worker process. The worker publishes its events on
// this PostgreSQL channel of the meta-database and every API server relays
// them to its own hub.
const relayChannel = "metapus_ws"

// maxRelayPayload keeps an event under PostgreSQL's NOTIFY limit (the payload
// must be shorter than 8000 bytes). A larger event is not pushed: the client
// still sees it on its next poll of the API.
const maxRelayPayload = 7900

const (
	relayPublishTimeout = 5 * time.Second
	relayRetryMin       = time.Second
	relayRetryMax       = 30 * time.Second
)

type relayEvent struct {
	TenantID string          `json:"t"`
	UserID   string          `json:"u"`
	Type     string          `json:"e"`
	Payload  json.RawMessage `json:"p"`
}

// NotifyBroadcaster publishes events for the API servers' hubs through the
// meta-database. It implements automation.Broadcaster for processes that hold
// no WebSocket connections themselves (the worker).
type NotifyBroadcaster struct {
	pool *pgxpool.Pool
}

// NewNotifyBroadcaster publishes through pool, a meta-database pool.
func NewNotifyBroadcaster(pool *pgxpool.Pool) *NotifyBroadcaster {
	return &NotifyBroadcaster{pool: pool}
}

// BroadcastToUser publishes the event; delivery is best-effort, like the
// hub's own: failures are logged, never returned.
func (b *NotifyBroadcaster) BroadcastToUser(tenantID, userID string, eventType string, payload any) {
	ctx := context.Background()
	body, err := encodeRelayEvent(tenantID, userID, eventType, payload)
	if err != nil {
		logger.Warn(ctx, "websocket relay: event not published", "event_type", eventType, "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, relayPublishTimeout)
	defer cancel()
	if _, err := b.pool.Exec(ctx, "SELECT pg_notify($1, $2)", relayChannel, string(body)); err != nil {
		logger.Warn(ctx, "websocket relay: publish failed", "event_type", eventType, "error", err)
	}
}

var errRelayEventTooLarge = errors.New("event exceeds the NOTIFY payload limit")

func encodeRelayEvent(tenantID, userID, eventType string, payload any) ([]byte, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(relayEvent{TenantID: tenantID, UserID: userID, Type: eventType, Payload: rawPayload})
	if err != nil {
		return nil, err
	}
	if len(body) > maxRelayPayload {
		return nil, errRelayEventTooLarge
	}
	return body, nil
}

// RunRelay listens on the meta-database for events the worker published and
// delivers them to hub until ctx is done. It holds a connection of its own,
// outside any pool, and reconnects with backoff when the connection drops.
func RunRelay(ctx context.Context, config *pgx.ConnConfig, hub *Hub) {
	retry := relayRetryMin
	for ctx.Err() == nil {
		listening, err := listenRelay(ctx, config, hub)
		if ctx.Err() != nil {
			return
		}
		if listening {
			retry = relayRetryMin
		}
		logger.Warn(ctx, "websocket relay disconnected", "error", err, "retry_after", retry)
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		retry = min(retry*2, relayRetryMax)
	}
}

// listenRelay reports whether it got as far as listening, so a connection
// that worked resets the backoff.
func listenRelay(ctx context.Context, config *pgx.ConnConfig, hub *Hub) (bool, error) {
	conn, err := pgx.ConnectConfig(ctx, config.Copy())
	if err != nil {
		return false, err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), relayPublishTimeout)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if _, err := conn.Exec(ctx, "LISTEN "+relayChannel); err != nil {
		return false, err
	}
	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, err
		}
		deliverRelayEvent(ctx, hub, notification.Payload)
	}
}

func deliverRelayEvent(ctx context.Context, hub *Hub, body string) {
	var event relayEvent
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		logger.Warn(ctx, "websocket relay: malformed event", "error", err)
		return
	}
	hub.BroadcastToUser(event.TenantID, event.UserID, event.Type, event.Payload)
}
