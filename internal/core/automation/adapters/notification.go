package adapters

import (
	"context"
	"encoding/json"
	"fmt"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/automation"
	"metapus/internal/core/id"
	"metapus/internal/domain/notifications"
	"metapus/pkg/logger"
)

// InternalNotificationProvider is the action type name.
const InternalNotificationProvider = "internal_notification"

// ActionPayload represents the expected JSON structure from the Rendered Template.
type ActionPayload struct {
	TargetUsers []string               `json:"target_users"`
	Title       string                 `json:"title"`
	Message     string                 `json:"message"`
	Link        string                 `json:"link,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
}

// InternalNotificationAdapter implements the automation.Adapter interface
// for sending in-app notifications and real-time websocket pushes.
type InternalNotificationAdapter struct {
	repo        notifications.Repository
	broadcaster automation.Broadcaster
}

// NewInternalNotificationAdapter creates a new InternalNotificationAdapter.
func NewInternalNotificationAdapter(repo notifications.Repository, broadcaster automation.Broadcaster) *InternalNotificationAdapter {
	return &InternalNotificationAdapter{
		repo:        repo,
		broadcaster: broadcaster,
	}
}

// Execute processes the rendered payload and dispatches notifications.
func (a *InternalNotificationAdapter) Execute(ctx context.Context, config map[string]interface{}, credentials []byte, payload string) error {
	var act ActionPayload
	if err := json.Unmarshal([]byte(payload), &act); err != nil {
		return fmt.Errorf("failed to unmarshal notification payload: %w", err)
	}

	if len(act.TargetUsers) == 0 {
		return fmt.Errorf("no target users specified in notification payload")
	}

	notifs := make([]*notifications.Notification, 0, len(act.TargetUsers))

	// Create notification entities
	for _, userStr := range act.TargetUsers {
		userID, err := id.Parse(userStr)
		if err != nil {
			logger.Warn(ctx, "invalid user ID in target_users, skipping", "userId", userStr, "error", err)
			continue
		}

		notifID := id.New()
		notif := &notifications.Notification{
			ID:         &notifID,
			UserID:     userID,
			Title:      act.Title,
			Message:    act.Message,
			Link:       &act.Link,
			Attributes: act.Attributes,
			IsRead:     false,
		}

		notifs = append(notifs, notif)
	}

	if len(notifs) == 0 {
		return nil // Nothing to do
	}

	// 1. Save to DB
	if err := a.repo.CreateBatch(ctx, notifs); err != nil {
		return fmt.Errorf("failed to save notifications to DB: %w", err)
	}

	// 2. Push to WebSocket Hub via Broadcaster interface
	tenantID := appctx.GetTenantID(ctx)
	for _, n := range notifs {
		a.broadcaster.BroadcastToUser(tenantID, n.UserID.String(), "notification", n)
	}

	return nil
}
