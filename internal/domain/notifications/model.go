package notifications

import (
	"context"
	"time"

	"metapus/internal/core/id"
)

// Severity defines the visual importance level of a notification.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
	SeveritySuccess Severity = "success"
)

// Notification represents an in-app message targeted to a specific user.
type Notification struct {
	ID           *id.ID                 `json:"id"`
	UserID       id.ID                  `json:"userId"`
	Title        string                 `json:"title"`
	Message      string                 `json:"message"`
	Severity     Severity               `json:"severity"`
	Link         *string                `json:"link,omitempty"`
	IsRead       bool                   `json:"isRead"`
	Attributes   map[string]interface{} `json:"attributes,omitempty"`
	Version      int                    `json:"version"`
	DeletionMark bool                   `json:"deletionMark"`
	CreatedAt    time.Time              `json:"createdAt"`
	UpdatedAt    time.Time              `json:"updatedAt"`
}

// NotificationFilter represents criteria for retrieving notifications.
type NotificationFilter struct {
	UserID     id.ID `json:"userId"`
	UnreadOnly bool  `json:"unreadOnly"`
	Limit      int   `json:"limit"`
	Offset     int   `json:"offset"`
}

// Repository defines data access methods for notifications.
type Repository interface {
	// Create inserts a new notification.
	Create(ctx context.Context, n *Notification) error

	// CreateBatch inserts multiple notifications efficiently.
	CreateBatch(ctx context.Context, notifications []*Notification) error

	// GetByID retrieves a single notification.
	GetByID(ctx context.Context, id id.ID) (*Notification, error)

	// List retrieves a list of notifications for a user based on the filter.
	List(ctx context.Context, filter *NotificationFilter) ([]*Notification, error)

	// CountUnread returns the number of unread notifications for a user.
	CountUnread(ctx context.Context, userID id.ID) (int, error)

	// MarkAsRead marks a specific notification as read.
	MarkAsRead(ctx context.Context, id id.ID, userID id.ID) error

	// MarkAsUnread marks a specific notification as unread.
	MarkAsUnread(ctx context.Context, id id.ID, userID id.ID) error

	// MarkAllAsRead marks all unread notifications for a user as read.
	MarkAllAsRead(ctx context.Context, userID id.ID) error

	// Delete soft-deletes a notification (sets deletion_mark = true).
	Delete(ctx context.Context, id id.ID, userID id.ID) error
}
