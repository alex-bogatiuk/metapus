package automation

// Broadcaster abstracts real-time message delivery to connected users.
// This interface lives in the core layer to avoid importing infrastructure/websocket.
type Broadcaster interface {
	BroadcastToUser(tenantID, userID string, eventType string, payload interface{})
}
