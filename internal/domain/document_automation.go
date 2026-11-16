package domain

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	core_entity "metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/tx"
	"metapus/internal/core/types"
	"metapus/pkg/logger"
)

// CurrencyInfo holds minimal currency metadata for event enrichment.
type CurrencyInfo struct {
	DecimalPlaces int    `json:"decimalPlaces"`
	Symbol        string `json:"symbol"`
	Name          string `json:"name"`
}

// CurrencyMetadataResolver resolves currency metadata by ID.
// Used by DocumentOutboxDecorator to enrich event payloads.
type CurrencyMetadataResolver interface {
	ResolveCurrency(ctx context.Context, currencyID id.ID) (*CurrencyInfo, error)
}

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
	next             DocumentService[T]
	publisher        OutboxPublisher
	entityName       string
	currencyResolver CurrencyMetadataResolver
}

// WithOutboxEvents returns a ServiceMiddleware that records successfully completed
// business operations to the transactional outbox system for automation.
func WithOutboxEvents[T any](entityName string, publisher OutboxPublisher, currencyResolver CurrencyMetadataResolver) ServiceMiddleware[T] {
	return func(next DocumentService[T]) DocumentService[T] {
		if publisher == nil {
			return next
		}
		return &DocumentOutboxDecorator[T]{next: next, publisher: publisher, entityName: entityName, currencyResolver: currencyResolver}
	}
}

// buildEvent creates a DomainEvent from the entity and action.
func (d *DocumentOutboxDecorator[T]) buildEvent(ctx context.Context, action string, entity T) *DomainEvent {
	eid := extractID(entity)
	if eid == nil {
		return nil
	}

	eventType := action // "posted", "created", "updated", etc.

	payload := map[string]any{
		"entityType": d.entityName,
		"entityId":   eid.String(),
		"action":     action,
		"doc":        entity,
	}

	// Enrich with currency metadata if the entity is CurrencyAware
	var currencyInfo *CurrencyInfo
	if currAware, ok := any(entity).(core_entity.ICurrencyAware); ok && d.currencyResolver != nil {
		currID := currAware.GetCurrencyID()
		if !id.IsNil(currID) {
			if info, err := d.currencyResolver.ResolveCurrency(ctx, currID); err == nil {
				currencyInfo = info
				payload["currency"] = info
			} else {
				logger.Warn(ctx, "failed to resolve currency metadata for outbox event", "currencyId", currID, "error", err)
			}
		}
	}

	// Human-readable amounts are optional enrichment; raw document amounts remain exact.
	humanAmounts, err := extractHumanAmounts(entity, currencyInfo)
	if err != nil {
		logger.Warn(ctx, "failed to format outbox currency amounts", "error", err)
	}
	if len(humanAmounts) > 0 {
		payload["humanAmounts"] = humanAmounts
	}

	return &DomainEvent{
		AggregateType: d.entityName,
		AggregateID:   *eid,
		EventType:     eventType,
		Payload:       payload,
	}
}

// extractHumanAmounts uses reflection to find MinorUnits and RawAmount fields
// on the top-level struct and converts them to transport-safe values.
//
// MinorUnits → exact decimal string (divided by currency's decimal places): 150000 → "1500.00"
// RawAmount → canonical decimal string: 6000000 → "6000000"
//
// RawAmount stays in raw units because:
//   - its asset's scale is not part of the document (no decimalPlaces)
//   - a string preserves integers larger than JavaScript's safe range
//   - the divisor varies by asset and is resolved on frontend, not in templates
func extractHumanAmounts(entity any, currencyInfo *CurrencyInfo) (map[string]any, error) {
	result := make(map[string]any)

	val := reflect.ValueOf(entity)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return result, nil
	}

	typ := val.Type()

	minorUnitsType := reflect.TypeFor[types.MinorUnits]()
	rawAmountType := reflect.TypeFor[types.RawAmount]()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		// Get JSON tag name for the field
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" || jsonTag == "" {
			continue
		}

		// Extract just the name from tag (e.g., "totalAmount,omitempty" -> "totalAmount")
		name, _, _ := strings.Cut(jsonTag, ",")

		switch field.Type {
		case minorUnitsType:
			// Currency scaling preserves every minor unit at the template boundary.
			if currencyInfo == nil {
				continue
			}
			amount, err := types.FormatMinorUnits(fieldVal.Int(), currencyInfo.DecimalPlaces)
			if err != nil {
				return nil, fmt.Errorf("invalid currency metadata: %w", err)
			}
			result[name] = amount
		case rawAmountType:
			// RawAmount is a struct-backed immutable value object; keep its
			// canonical string representation at the JSON/template boundary.
			result[name] = fieldVal.Interface().(types.RawAmount).String()
		}
	}

	return result, nil
}

// mutate keeps the document and every required event in one transaction. A
// savepoint also rolls back this command when an outer caller handles its error.
func (d *DocumentOutboxDecorator[T]) mutate(ctx context.Context, fn func(context.Context) error) error {
	manager, err := tenant.GetTxManager(ctx)
	if err != nil {
		return fmt.Errorf("document outbox transaction: %w", err)
	}
	run := manager.RunInTransaction
	if nested, ok := manager.(tx.SavepointManager); ok {
		run = nested.RunInSavepoint
	}
	return run(ctx, fn)
}

func (d *DocumentOutboxDecorator[T]) emit(ctx context.Context, action string, entity T) error {
	event := d.buildEvent(ctx, action, entity)
	if event == nil {
		return fmt.Errorf("document outbox: %s entity has no ID", d.entityName)
	}
	if err := d.publisher.Publish(ctx, *event); err != nil {
		return fmt.Errorf("publish document %s event: %w", action, err)
	}
	return nil
}

func (d *DocumentOutboxDecorator[T]) Create(ctx context.Context, entity T) error {
	return d.mutate(ctx, func(ctx context.Context) error {
		if err := d.next.Create(ctx, entity); err != nil {
			return err
		}
		return d.emit(ctx, "created", entity)
	})
}

func (d *DocumentOutboxDecorator[T]) Update(ctx context.Context, entity T) error {
	return d.mutate(ctx, func(ctx context.Context) error {
		if err := d.next.Update(ctx, entity); err != nil {
			return err
		}
		return d.emit(ctx, "updated", entity)
	})
}

func (d *DocumentOutboxDecorator[T]) Delete(ctx context.Context, entityID id.ID) error {
	return d.mutate(ctx, func(ctx context.Context) error {
		if err := d.next.Delete(ctx, entityID); err != nil {
			return err
		}
		event := DomainEvent{AggregateType: d.entityName, AggregateID: entityID, EventType: "deleted",
			Payload: map[string]any{"entityType": d.entityName, "entityId": entityID.String(), "action": "deleted"}}
		if err := d.publisher.Publish(ctx, event); err != nil {
			return fmt.Errorf("publish document deleted event: %w", err)
		}
		return nil
	})
}

func (d *DocumentOutboxDecorator[T]) mutateByID(ctx context.Context, entityID id.ID, action string, mutate func(context.Context, id.ID) error) error {
	return d.mutate(ctx, func(ctx context.Context) error {
		if err := mutate(ctx, entityID); err != nil {
			return err
		}
		entity, err := d.next.GetByID(ctx, entityID)
		if err != nil {
			return fmt.Errorf("load document for %s event: %w", action, err)
		}
		return d.emit(ctx, action, entity)
	})
}

func (d *DocumentOutboxDecorator[T]) Post(ctx context.Context, entityID id.ID) error {
	return d.mutateByID(ctx, entityID, "posted", d.next.Post)
}

func (d *DocumentOutboxDecorator[T]) Unpost(ctx context.Context, entityID id.ID) error {
	return d.mutateByID(ctx, entityID, "unposted", d.next.Unpost)
}

func (d *DocumentOutboxDecorator[T]) PostAndSave(ctx context.Context, entity T) error {
	return d.mutate(ctx, func(ctx context.Context) error {
		if err := d.next.PostAndSave(ctx, entity); err != nil {
			return err
		}
		return d.emit(ctx, "posted", entity)
	})
}

func (d *DocumentOutboxDecorator[T]) UpdateAndRepost(ctx context.Context, entity T) error {
	return d.mutate(ctx, func(ctx context.Context) error {
		if err := d.next.UpdateAndRepost(ctx, entity); err != nil {
			return err
		}
		if err := d.emit(ctx, "updated", entity); err != nil {
			return err
		}
		return d.emit(ctx, "posted", entity)
	})
}

func (d *DocumentOutboxDecorator[T]) SetDeletionMark(ctx context.Context, entityID id.ID, marked bool) error {
	action := "deletion_cleared"
	if marked {
		action = "deletion_marked"
	}
	return d.mutateByID(ctx, entityID, action, func(ctx context.Context, uid id.ID) error {
		return d.next.SetDeletionMark(ctx, uid, marked)
	})
}

func (d *DocumentOutboxDecorator[T]) GetByID(ctx context.Context, entityID id.ID) (T, error) {
	return d.next.GetByID(ctx, entityID)
}

func (d *DocumentOutboxDecorator[T]) GetByIDForUpdate(ctx context.Context, entityID id.ID) (T, error) {
	return d.next.GetByIDForUpdate(ctx, entityID)
}

func (d *DocumentOutboxDecorator[T]) List(ctx context.Context, filter ListFilter) (CursorListResult[T], error) {
	return d.next.List(ctx, filter)
}

func (d *DocumentOutboxDecorator[T]) ListIDs(ctx context.Context, filter ListFilter, maxIDs int) ([]id.ID, error) {
	return d.next.ListIDs(ctx, filter, maxIDs)
}
