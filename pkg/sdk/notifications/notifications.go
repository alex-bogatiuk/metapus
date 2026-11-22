// Package notifications are the user notifications of the platform.
package notifications

import (
	core "metapus/internal/domain/notifications"
)

type (
	// Notification represents an in-app message targeted to a specific user.
	Notification = core.Notification
	// Severity defines the visual importance level of a notification.
	Severity = core.Severity
)

const (
	SeverityInfo    = core.SeverityInfo
	SeveritySuccess = core.SeveritySuccess
)
