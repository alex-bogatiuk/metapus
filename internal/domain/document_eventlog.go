package domain

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/eventlog"
	"metapus/internal/core/id"
	"metapus/pkg/logger"
)

// EventLogDocumentService is a decorator that records data events
// to sys_event_log for every mutating document operation.
type EventLogDocumentService[T any] struct {
	next       DocumentService[T]
	writer     eventlog.Writer
	entityName string
}

// WithEventLog returns a ServiceMiddleware that records events to the event log.
// If writer is nil, returns the service unchanged (no-op).
func WithEventLog[T any](entityName string, writer eventlog.Writer) ServiceMiddleware[T] {
	return func(next DocumentService[T]) DocumentService[T] {
		if writer == nil {
			return next
		}
		return &EventLogDocumentService[T]{next: next, writer: writer, entityName: entityName}
	}
}

// documentActionVerb maps event types to human-readable action verbs.
var documentActionVerb = map[eventlog.EventType]string{
	eventlog.EventDocumentCreate: "created",
	eventlog.EventDocumentUpdate: "updated",
	eventlog.EventDocumentDelete: "deleted",
	eventlog.EventDocumentPost:   "posted",
	eventlog.EventDocumentUnpost: "unposted",
}

func (s *EventLogDocumentService[T]) emit(ctx context.Context, eventType eventlog.EventType, entityID *id.ID, entityNumber string, start time.Time, err error) {
	severity := eventlog.SeverityInfo
	verb := documentActionVerb[eventType]
	if verb == "" {
		verb = string(eventType)
	}
	numSuffix := ""
	if entityNumber != "" {
		numSuffix = " " + entityNumber
	}
	msg := fmt.Sprintf("Document %s: %s%s", verb, s.entityName, numSuffix)
	if err != nil {
		severity = eventlog.SeverityError
		msg = fmt.Sprintf("Document %s failed: %s%s — %v", verb, s.entityName, numSuffix, err)
	}

	duration := int(time.Since(start).Milliseconds())
	event := eventlog.Event{
		Category:     eventlog.CategoryData,
		Severity:     severity,
		EventType:    eventType,
		EntityType:   s.entityName,
		EntityID:     entityID,
		EntityNumber: entityNumber,
		Message:      msg,
		DurationMs:   &duration,
	}

	if writeErr := s.writer.Write(ctx, event); writeErr != nil {
		logger.Warn(ctx, "eventlog: failed to write event",
			"entity", s.entityName,
			"eventType", eventType,
			"error", writeErr,
		)
	}
}

// extractID attempts to extract the ID from a generic entity using the GetID interface.
func extractID[T any](entity T) *id.ID {
	if getter, ok := any(entity).(interface{ GetID() id.ID }); ok {
		eid := getter.GetID()
		return &eid
	}
	return nil
}

// extractNumber attempts to extract the document number from a generic entity.
func extractNumber[T any](entity T) string {
	if getter, ok := any(entity).(interface{ GetNumber() string }); ok {
		return getter.GetNumber()
	}
	return ""
}

func (s *EventLogDocumentService[T]) Create(ctx context.Context, entity T) (err error) {
	start := time.Now()
	err = s.next.Create(ctx, entity)
	s.emit(ctx, eventlog.EventDocumentCreate, extractID(entity), extractNumber(entity), start, err)
	return
}

func (s *EventLogDocumentService[T]) GetByID(ctx context.Context, docID id.ID) (T, error) {
	return s.next.GetByID(ctx, docID)
}

func (s *EventLogDocumentService[T]) Update(ctx context.Context, entity T) (err error) {
	start := time.Now()
	err = s.next.Update(ctx, entity)
	s.emit(ctx, eventlog.EventDocumentUpdate, extractID(entity), extractNumber(entity), start, err)
	return
}

func (s *EventLogDocumentService[T]) Delete(ctx context.Context, docID id.ID) (err error) {
	start := time.Now()
	err = s.next.Delete(ctx, docID)
	s.emit(ctx, eventlog.EventDocumentDelete, &docID, "", start, err)
	return
}

func (s *EventLogDocumentService[T]) Post(ctx context.Context, docID id.ID) (err error) {
	start := time.Now()
	err = s.next.Post(ctx, docID)
	s.emit(ctx, eventlog.EventDocumentPost, &docID, "", start, err)
	return
}

func (s *EventLogDocumentService[T]) Unpost(ctx context.Context, docID id.ID) (err error) {
	start := time.Now()
	err = s.next.Unpost(ctx, docID)
	s.emit(ctx, eventlog.EventDocumentUnpost, &docID, "", start, err)
	return
}

func (s *EventLogDocumentService[T]) PostAndSave(ctx context.Context, entity T) (err error) {
	start := time.Now()
	err = s.next.PostAndSave(ctx, entity)
	s.emit(ctx, eventlog.EventDocumentPost, extractID(entity), extractNumber(entity), start, err)
	return
}

func (s *EventLogDocumentService[T]) UpdateAndRepost(ctx context.Context, entity T) (err error) {
	start := time.Now()
	err = s.next.UpdateAndRepost(ctx, entity)
	s.emit(ctx, eventlog.EventDocumentUpdate, extractID(entity), extractNumber(entity), start, err)
	return
}

func (s *EventLogDocumentService[T]) SetDeletionMark(ctx context.Context, docID id.ID, marked bool) (err error) {
	start := time.Now()
	err = s.next.SetDeletionMark(ctx, docID, marked)
	s.emit(ctx, eventlog.EventDocumentDelete, &docID, "", start, err)
	return
}

func (s *EventLogDocumentService[T]) List(ctx context.Context, filter ListFilter) (CursorListResult[T], error) {
	return s.next.List(ctx, filter)
}
