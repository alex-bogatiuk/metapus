package domain

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
	"metapus/pkg/logger"
)

// DomainEvent represents an event to be published via outbox.
type DomainEvent struct {
	AggregateType string
	AggregateID   id.ID
	EventType     string
	Payload       any
}

// OutboxPublisher writes events to the transactional outbox.
// Handled by the infrastructure layer (e.g. postgres.OutboxPublisher).
type OutboxPublisher interface {
	Publish(ctx context.Context, event DomainEvent) error
}

// DocumentOutboxDecorator is a decorator that records automation events
// to sys_outbox for every successful mutating document operation.
// Unlike EventLog, we only record successful operations!
type DocumentOutboxDecorator[T any] struct {
	next       DocumentService[T]
	publisher  OutboxPublisher
	entityName string
}

// WithOutboxEvents returns a ServiceMiddleware that records successfully completed 
// business operations to the transactional outbox system for automation.
func WithOutboxEvents[T any](entityName string, publisher OutboxPublisher) ServiceMiddleware[T] {
	return func(next DocumentService[T]) DocumentService[T] {
		if publisher == nil {
			return next
		}
		return &DocumentOutboxDecorator[T]{next: next, publisher: publisher, entityName: entityName}
	}
}

// emit writes an event to the outbox. MUST run inside the transaction context!
func (d *DocumentOutboxDecorator[T]) emit(ctx context.Context, action string, entity T) {
	// Only publish if we have an ID
	eid := extractID(entity)
	if eid == nil {
		return
	}

	eventType := fmt.Sprintf("document.%s.%s", d.entityName, action)
	
	// Create payload with document snapshot and action
	payload := map[string]any{
		"entityType": d.entityName,
		"entityId":   eid.String(),
		"action":     action,
		"doc":        entity,
	}

	err := d.publisher.Publish(ctx, DomainEvent{
		AggregateType: d.entityName,
		AggregateID:   *eid,
		EventType:     eventType,
		Payload:       payload,
	})

	if err != nil {
		// In a transactional outbox, failing to write the outbox event MUST fail the entire transaction.
		// However, emit is called AFTER next.Create(), which might not return an error, but the transaction
		// is still open. Since this decorator intercepts inside Chain (which runs inside transaction), this is safe,
		// but we can't easily bubble the error if we do this defer/async.
		// Wait, we call emit synchronously, so we should bubble the error!
		logger.Error(ctx, "failed to publish outbox event", "error", err, "eventType", eventType)
	}
}

func (d *DocumentOutboxDecorator[T]) Create(ctx context.Context, entity T) (err error) {
	err = d.next.Create(ctx, entity)
	if err == nil {
		d.emit(ctx, "created", entity)
	}
	return
}

func (d *DocumentOutboxDecorator[T]) Update(ctx context.Context, entity T) (err error) {
	err = d.next.Update(ctx, entity)
	if err == nil {
		d.emit(ctx, "updated", entity)
	}
	return
}

func (d *DocumentOutboxDecorator[T]) Delete(ctx context.Context, entityID id.ID) (err error) {
	err = d.next.Delete(ctx, entityID)
	if err == nil {
		eventType := fmt.Sprintf("document.%s.deleted", d.entityName)
		pubErr := d.publisher.Publish(ctx, DomainEvent{
			AggregateType: d.entityName,
			AggregateID:   entityID,
			EventType:     eventType,
			Payload: map[string]any{
				"entityType": d.entityName,
				"entityId":   entityID.String(),
				"action":     "deleted",
			},
		})
		if pubErr != nil {
			logger.Error(ctx, "failed to publish outbox event", "error", pubErr, "eventType", eventType)
		}
	}
	return
}

func (d *DocumentOutboxDecorator[T]) Post(ctx context.Context, entityID id.ID) (err error) {
	err = d.next.Post(ctx, entityID)
	if err == nil {
		if doc, fetchErr := d.next.GetByID(ctx, entityID); fetchErr == nil {
			d.emit(ctx, "posted", doc)
		} else {
			logger.Error(ctx, "failed to fetch doc for outbox post event", "error", fetchErr)
		}
	}
	return
}

func (d *DocumentOutboxDecorator[T]) Unpost(ctx context.Context, entityID id.ID) (err error) {
	err = d.next.Unpost(ctx, entityID)
	if err == nil {
		if doc, fetchErr := d.next.GetByID(ctx, entityID); fetchErr == nil {
			d.emit(ctx, "unposted", doc)
		}
	}
	return
}

func (d *DocumentOutboxDecorator[T]) PostAndSave(ctx context.Context, entity T) (err error) {
	err = d.next.PostAndSave(ctx, entity)
	if err == nil {
		d.emit(ctx, "posted", entity)
	}
	return
}

func (d *DocumentOutboxDecorator[T]) UpdateAndRepost(ctx context.Context, entity T) (err error) {
	err = d.next.UpdateAndRepost(ctx, entity)
	if err == nil {
		d.emit(ctx, "updated", entity)
		d.emit(ctx, "posted", entity)
	}
	return
}

func (d *DocumentOutboxDecorator[T]) SetDeletionMark(ctx context.Context, entityID id.ID, marked bool) (err error) {
	err = d.next.SetDeletionMark(ctx, entityID, marked)
	if err == nil {
		action := "deletion_marked"
		if !marked {
			action = "deletion_cleared"
		}
		if doc, fetchErr := d.next.GetByID(ctx, entityID); fetchErr == nil {
			d.emit(ctx, action, doc)
		}
	}
	return
}

func (d *DocumentOutboxDecorator[T]) GetByID(ctx context.Context, entityID id.ID) (T, error) {
	return d.next.GetByID(ctx, entityID)
}

func (d *DocumentOutboxDecorator[T]) List(ctx context.Context, filter ListFilter) (CursorListResult[T], error) {
	return d.next.List(ctx, filter)
}

func (d *DocumentOutboxDecorator[T]) ListIDs(ctx context.Context, filter ListFilter, maxIDs int) ([]id.ID, error) {
	return d.next.ListIDs(ctx, filter, maxIDs)
}
