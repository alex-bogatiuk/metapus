package domain

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/eventlog"
	"metapus/internal/core/id"
	"metapus/pkg/logger"
)

// EventLogCatalogService is a decorator that records data events
// to sys_event_log for every mutating catalog operation.
// It wraps a CatalogService (not an interface — catalogs don't have a middleware chain).
type EventLogCatalogService[T entity.CatalogEntity] struct {
	inner      *CatalogService[T]
	writer     eventlog.Writer
	entityName string
}

// NewEventLogCatalogService registers event log hooks on a CatalogService.
// If writer is nil, this is a no-op.
func NewEventLogCatalogService[T entity.CatalogEntity](svc *CatalogService[T], entityName string, writer eventlog.Writer) {
	if writer == nil {
		return
	}
	wrapper := &EventLogCatalogService[T]{inner: svc, writer: writer, entityName: entityName}

	// Register after-hooks on the inner service to capture events.
	// This uses the existing HookRegistry pattern — no new interfaces needed.
	svc.Hooks().OnAfterCreate(func(ctx context.Context, ent T) error {
		eid := ent.GetID()
		wrapper.emit(ctx, eventlog.EventCatalogCreate, &eid, time.Now(), nil)
		return nil
	})
	svc.Hooks().OnAfterUpdate(func(ctx context.Context, ent T) error {
		eid := ent.GetID()
		wrapper.emit(ctx, eventlog.EventCatalogUpdate, &eid, time.Now(), nil)
		return nil
	})
	svc.Hooks().OnAfterDelete(func(ctx context.Context, ent T) error {
		eid := ent.GetID()
		wrapper.emit(ctx, eventlog.EventCatalogDelete, &eid, time.Now(), nil)
		return nil
	})
}

// actionVerb maps event types to human-readable action verbs.
var catalogActionVerb = map[eventlog.EventType]string{
	eventlog.EventCatalogCreate: "created",
	eventlog.EventCatalogUpdate: "updated",
	eventlog.EventCatalogDelete: "deleted",
}

func (s *EventLogCatalogService[T]) emit(ctx context.Context, eventType eventlog.EventType, entityID *id.ID, start time.Time, err error) {
	severity := eventlog.SeverityInfo
	verb := catalogActionVerb[eventType]
	if verb == "" {
		verb = string(eventType)
	}
	msg := fmt.Sprintf("Catalog %s: %s", verb, s.entityName)
	if err != nil {
		severity = eventlog.SeverityError
		msg = fmt.Sprintf("Catalog %s failed: %s — %v", verb, s.entityName, err)
	}

	duration := int(time.Since(start).Milliseconds())
	event := eventlog.Event{
		Category:   eventlog.CategoryData,
		Severity:   severity,
		EventType:  eventType,
		EntityType: s.entityName,
		EntityID:   entityID,
		Message:    msg,
		DurationMs: &duration,
	}

	if writeErr := s.writer.Write(ctx, event); writeErr != nil {
		logger.Warn(ctx, "eventlog: failed to write catalog event",
			"entity", s.entityName,
			"eventType", eventType,
			"error", writeErr,
		)
	}
}

// (extractCatalogID removed as it is no longer needed)
