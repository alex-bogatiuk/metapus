package domain

import (
	"context"
	"math"
	"reflect"
	"strings"

	core_entity "metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
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

	// Extract top-level MinorUnits fields into human-readable floats
	humanAmounts := extractHumanAmounts(entity, currencyInfo)
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

// extractHumanAmounts uses reflection to find MinorUnits and CryptoAmount fields
// on the top-level struct and converts them to human-readable numeric values.
//
// MinorUnits → float64 (divided by currency's decimal places): 150000 → 1500.00
// CryptoAmount → int64 (raw minor units, no division): 6000000 → 6000000
//
// CryptoAmount stays as int64 because:
//   - crypto invoices have no currency metadata (no decimalPlaces)
//   - templates render int64 as "6000000", but float64 as "6e+06" (scientific notation bug)
//   - divisor varies by token and is resolved on frontend, not in templates
func extractHumanAmounts(entity any, currencyInfo *CurrencyInfo) map[string]any {
	result := make(map[string]any)

	val := reflect.ValueOf(entity)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return result
	}

	typ := val.Type()

	// Default to 2 decimal places if no currency metadata
	decimalPlaces := 2
	if currencyInfo != nil {
		decimalPlaces = currencyInfo.DecimalPlaces
	}

	divisor := math.Pow10(decimalPlaces)

	minorUnitsType := reflect.TypeFor[types.MinorUnits]()
	cryptoAmountType := reflect.TypeFor[types.CryptoAmount]()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		// Get JSON tag name for the field
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" || jsonTag == "" {
			continue
		}

		// Extract just the name from tag (e.g., "totalAmount,omitempty" -> "totalAmount")
		name := strings.Split(jsonTag, ",")[0]

		switch field.Type {
		case minorUnitsType:
			// MinorUnits: convert to human-readable float via currency scaling
			result[name] = float64(fieldVal.Int()) / divisor
		case cryptoAmountType:
			// CryptoAmount: raw int64 — no scaling, avoids scientific notation
			result[name] = fieldVal.Int()
		}
	}

	return result
}

// emitInOwnTx writes an outbox event in its OWN transaction.
// Required for Post/Unpost/PostAndSave/UpdateAndRepost because PostingEngine
// opens and commits its own transaction, so by the time the decorator's method
// runs, there is no open transaction for the outbox INSERT.
func (d *DocumentOutboxDecorator[T]) emitInOwnTx(ctx context.Context, action string, entity T) {
	ev := d.buildEvent(ctx, action, entity)
	if ev == nil {
		return
	}

	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		logger.Error(ctx, "failed to get tx manager for outbox event", "error", err, "eventType", ev.EventType)
		return
	}

	if txErr := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return d.publisher.Publish(txCtx, *ev)
	}); txErr != nil {
		logger.Error(ctx, "failed to publish outbox event in own tx", "error", txErr, "eventType", ev.EventType)
	}
}

func (d *DocumentOutboxDecorator[T]) Create(ctx context.Context, entity T) (err error) {
	err = d.next.Create(ctx, entity)
	if err == nil {
		d.emitInOwnTx(ctx, "created", entity)
	}
	return
}

func (d *DocumentOutboxDecorator[T]) Update(ctx context.Context, entity T) (err error) {
	err = d.next.Update(ctx, entity)
	if err == nil {
		d.emitInOwnTx(ctx, "updated", entity)
	}
	return
}

func (d *DocumentOutboxDecorator[T]) Delete(ctx context.Context, entityID id.ID) (err error) {
	err = d.next.Delete(ctx, entityID)
	if err == nil {
		ev := &DomainEvent{
			AggregateType: d.entityName,
			AggregateID:   entityID,
			EventType:     "deleted",
			Payload: map[string]any{
				"entityType": d.entityName,
				"entityId":   entityID.String(),
				"action":     "deleted",
			},
		}
		txm, txErr := tenant.GetTxManager(ctx)
		if txErr != nil {
			logger.Error(ctx, "failed to get tx manager for outbox delete event", "error", txErr)
			return
		}
		if pubErr := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
			return d.publisher.Publish(txCtx, *ev)
		}); pubErr != nil {
			logger.Error(ctx, "failed to publish outbox delete event", "error", pubErr)
		}
	}
	return
}

func (d *DocumentOutboxDecorator[T]) Post(ctx context.Context, entityID id.ID) (err error) {
	err = d.next.Post(ctx, entityID)
	if err == nil {
		// PostingEngine commits its own tx, so we emit in a separate tx.
		if doc, fetchErr := d.next.GetByID(ctx, entityID); fetchErr == nil {
			d.emitInOwnTx(ctx, "posted", doc)
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
			d.emitInOwnTx(ctx, "unposted", doc)
		}
	}
	return
}

func (d *DocumentOutboxDecorator[T]) PostAndSave(ctx context.Context, entity T) (err error) {
	err = d.next.PostAndSave(ctx, entity)
	if err == nil {
		d.emitInOwnTx(ctx, "posted", entity)
	}
	return
}

func (d *DocumentOutboxDecorator[T]) UpdateAndRepost(ctx context.Context, entity T) (err error) {
	err = d.next.UpdateAndRepost(ctx, entity)
	if err == nil {
		d.emitInOwnTx(ctx, "updated", entity)
		d.emitInOwnTx(ctx, "posted", entity)
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
		// SetDeletionMark may go through PostingEngine.Unpost, so use own tx.
		if doc, fetchErr := d.next.GetByID(ctx, entityID); fetchErr == nil {
			d.emitInOwnTx(ctx, action, doc)
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
