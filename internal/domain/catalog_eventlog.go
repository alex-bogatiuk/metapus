package domain

import (
	"context"
	"fmt"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/domain/eventlog"
)

// NewEventLogCatalogService records successful mutations in the same transaction.
// Writer errors are mandatory failures; the service rolls back the mutation.
func NewEventLogCatalogService[T entity.CatalogEntity](svc *CatalogService[T], entityName string, writer eventlog.Writer) {
	if writer == nil {
		return
	}
	for _, phase := range []struct {
		hook  HookEvent
		event eventlog.EventType
	}{
		{TransactionCreate, eventlog.EventCatalogCreate},
		{TransactionUpdate, eventlog.EventCatalogUpdate},
		{TransactionDelete, eventlog.EventCatalogDelete},
		{TransactionSetDeletionMark, eventlog.EventCatalogSetDeletionMark},
		{TransactionClearDeletionMark, eventlog.EventCatalogClearDeletionMark},
	} {
		svc.Hooks().On(phase.hook, func(ctx context.Context, ent T) error {
			uid := ent.GetID()
			details := map[string]any{"operation": string(phase.event)}
			if versioned, ok := any(ent).(interface{ GetVersion() int }); ok {
				details["version"] = versioned.GetVersion()
			}
			// Explicit metadata only: snapshots, names, attributes and credentials never
			// enter the audit payload. Actor and version identify the successful change.
			return writer.Write(ctx, eventlog.Event{
				Category: eventlog.CategoryData, Severity: eventlog.SeverityInfo,
				EventType: phase.event, EntityType: entityName, EntityID: &uid,
				UserID: appctx.GetUserID(ctx), Details: details,
				Message: fmt.Sprintf("%s: %s", entityName, phase.event),
			})
		})
	}
}
