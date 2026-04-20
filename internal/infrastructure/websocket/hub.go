package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"metapus/pkg/logger"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     checkOrigin,
}

// checkOrigin validates the Origin header against allowed origins.
// In development (APP_ENV=development or empty), all origins are allowed.
func checkOrigin(r *http.Request) bool {
	env := os.Getenv("APP_ENV")
	if env == "" || env == "development" {
		return true
	}

	allowed := os.Getenv("APP_ALLOWED_ORIGINS")
	if allowed == "" {
		return true // No restriction configured
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // Non-browser clients
	}

	for _, o := range strings.Split(allowed, ",") {
		if strings.TrimSpace(o) == origin {
			return true
		}
	}
	return false
}

// Hub manages active WebSocket connections.
type Hub struct {
	// connections mapped by tenantID -> userID -> set of connections
	conns map[string]map[string]map[*Connection]struct{}
	mu    sync.RWMutex
}

// GlobalHub is the singleton instance used in production.
var GlobalHub = &Hub{
	conns: make(map[string]map[string]map[*Connection]struct{}),
}

// BroadcastToUser implements automation.Broadcaster interface.
// It sends a JSON payload to all active connections of a specific user.
func (h *Hub) BroadcastToUser(tenantID, userID string, eventType string, payload interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	userConns, ok := h.conns[tenantID][userID]
	if !ok || len(userConns) == 0 {
		return
	}

	msg := map[string]interface{}{
		"type":    eventType,
		"payload": payload,
		"time":    time.Now().UnixMilli(),
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		logger.Error(context.Background(), "Failed to marshal websocket message", "error", err)
		return
	}

	for c := range userConns {
		select {
		case c.Send <- msgBytes:
		default:
			// Buffer full or closed — drop message for this connection
		}
	}
}

// Connection represents an active WebSocket connection.
type Connection struct {
	TenantID string
	UserID   string
	Conn     *websocket.Conn
	Send     chan []byte
	hub      *Hub // injected via DI, not a global reference
}

func (h *Hub) Register(c *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.conns[c.TenantID] == nil {
		h.conns[c.TenantID] = make(map[string]map[*Connection]struct{})
	}
	if h.conns[c.TenantID][c.UserID] == nil {
		h.conns[c.TenantID][c.UserID] = make(map[*Connection]struct{})
	}
	h.conns[c.TenantID][c.UserID][c] = struct{}{}
}

func (h *Hub) Unregister(c *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.conns[c.TenantID][c.UserID][c]; ok {
		delete(h.conns[c.TenantID][c.UserID], c)
		close(c.Send)

		if len(h.conns[c.TenantID][c.UserID]) == 0 {
			delete(h.conns[c.TenantID], c.UserID)
		}
		if len(h.conns[c.TenantID]) == 0 {
			delete(h.conns, c.TenantID)
		}
	}
}

// writePump pushes messages from the Send channel to the websocket connection.
// Each message is sent as a separate WebSocket TextMessage to ensure
// the frontend can JSON.parse each event individually.
func (c *Connection) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.Send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump consumes messages from the connection (keeps alive, detects close).
func (c *Connection) readPump() {
	defer func() {
		c.hub.Unregister(c) // Use injected hub, not global
		_ = c.Conn.Close()
	}()
	c.Conn.SetReadLimit(512)
	_ = c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		_ = c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Error(context.Background(), "Websocket unexpected close error", "error", err)
			}
			break
		}
	}
}

// ServeWS upgrades the HTTP connection to a WebSocket and registers it.
func ServeWS(w http.ResponseWriter, r *http.Request, tenantID, userID string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(r.Context(), "Failed to upgrade websocket connection", "error", err)
		return
	}

	c := &Connection{
		TenantID: tenantID,
		UserID:   userID,
		Conn:     conn,
		Send:     make(chan []byte, 256),
		hub:      GlobalHub, // Inject hub instance
	}

	GlobalHub.Register(c)

	go c.writePump()
	go c.readPump()
}
