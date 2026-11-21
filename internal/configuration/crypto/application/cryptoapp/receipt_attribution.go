package cryptoapp

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
)

// A manual attribution of a receipt (merchant credit or platform funding) is
// prepared by one operator and decided by another. Both kinds serialize on the
// receipt remainder and announce their transitions the same way.

type receiptAttribution interface {
	GetID() id.ID
	GetVersion() int
	AttributedReceipt() id.ID
}

type receiptAttributionStore[D receiptAttribution] interface {
	LockReceiptRemainder(context.Context, id.ID) (ReceiptRemainder, error)
	GetByID(context.Context, id.ID) (D, error)
	GetByIDForUpdate(context.Context, id.ID) (D, error)
}

// decideReceiptAttribution locks the receipt remainder before the document,
// the order preparation uses, then applies the decision to the locked document.
// A positive expectedVersion must match the stored one.
func decideReceiptAttribution[D receiptAttribution](ctx context.Context, txm tx.Manager, store receiptAttributionStore[D],
	docType string, docID id.ID, expectedVersion int, apply func(context.Context, D, ReceiptRemainder) error) (D, error) {
	var result D
	err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		current, err := store.GetByID(ctx, docID)
		if err != nil {
			return err
		}
		remainder, err := store.LockReceiptRemainder(ctx, current.AttributedReceipt())
		if err != nil {
			return err
		}
		doc, err := store.GetByIDForUpdate(ctx, docID)
		if err != nil {
			return err
		}
		if expectedVersion > 0 && doc.GetVersion() != expectedVersion {
			return apperror.NewConcurrentModification(docType, docID)
		}
		if err := apply(ctx, doc, remainder); err != nil {
			return err
		}
		result = doc
		return nil
	})
	return result, err
}

type receiptAttributionEvent struct {
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
	Action     string `json:"action"`
	Doc        any    `json:"doc"`
}

// publishReceiptAttribution announces a transition of an attribution document.
func publishReceiptAttribution(ctx context.Context, outbox domain.OutboxPublisher, docType string, doc receiptAttribution, action string) error {
	return outbox.Publish(ctx, domain.DomainEvent{
		AggregateType: docType, AggregateID: doc.GetID(), EventType: action,
		Payload: receiptAttributionEvent{EntityType: docType, EntityID: doc.GetID().String(), Action: action, Doc: doc},
	})
}
