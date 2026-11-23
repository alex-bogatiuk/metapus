// Package eventlog writes the tenant's event log.
package eventlog

import (
	core "metapus/internal/domain/eventlog"
)

type (
	// Event represents a single entry in the system event log.
	Event = core.Event
	// EventType is a typed string for event classification.
	// Constants are defined in Go code; the DB column is VARCHAR(50).
	EventType = core.EventType
	// Writer records events into the event log.
	Writer = core.Writer
)

const (
	CategoryBackground  = core.CategoryBackground
	CategoryData        = core.CategoryData
	CategorySecurity    = core.CategorySecurity
	CategorySystem      = core.CategorySystem
	EventDocumentUpdate = core.EventDocumentUpdate
	SeverityCritical    = core.SeverityCritical
	SeverityError       = core.SeverityError
	SeverityInfo        = core.SeverityInfo
	SeverityWarning     = core.SeverityWarning
)
