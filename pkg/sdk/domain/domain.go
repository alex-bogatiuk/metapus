// Package domain holds the platform's catalog and document services a
// configuration builds its own on: hooks, deletion, references, events and
// outbox publishing.
package domain

import (
	"metapus/internal/core/entity"
	core "metapus/internal/domain"
	"metapus/internal/domain/eventlog"
)

type (
	// BaseDocumentService provides generic business logic for document entities.
	// Implements the Template Method pattern: common operations are defined here,
	// document-specific behaviour is provided via interfaces on T.
	BaseDocumentService[T core.DocumentEntity[L], L any] = core.BaseDocumentService[T, L]
	// BaseDocumentServiceConfig configures the BaseDocumentService.
	BaseDocumentServiceConfig[T core.DocumentEntity[L], L any] = core.BaseDocumentServiceConfig[T, L]
	// BaseHeaderDocumentService provides generic business logic for documents
	// whose header is one row (e.g., CryptoInvoice, CryptoPayment). Unlike
	// BaseDocumentService it is not generic over one line type: table parts, any
	// number of them, are loaded and stored with the header through Parts.
	//
	// Implements DocumentService[T] — handlers and decorators are agnostic to this distinction.
	BaseHeaderDocumentService[T core.HeaderDocumentEntity] = core.BaseHeaderDocumentService[T]
	// BaseHeaderDocumentServiceConfig configures the BaseHeaderDocumentService.
	BaseHeaderDocumentServiceConfig[T core.HeaderDocumentEntity] = core.BaseHeaderDocumentServiceConfig[T]
	// CatalogRepository defines CRUD operations for catalog entities.
	CatalogRepository[T entity.CatalogEntity] = core.CatalogRepository[T]
	// CatalogService provides business logic for catalog entities.
	// In Database-per-Tenant architecture, TxManager can be nil - it will be obtained from context.
	CatalogService[T entity.CatalogEntity] = core.CatalogService[T]
	// CatalogServiceConfig configures the catalog service.
	CatalogServiceConfig[T entity.CatalogEntity] = core.CatalogServiceConfig[T]
	// CursorListResult contains cursor-paginated results.
	CursorListResult[T any] = core.CursorListResult[T]
	// DeleteMarkedResult is the result of executing a deletion plan.
	DeleteMarkedResult = core.DeleteMarkedResult
	// DeletedObjectResult reports the outcome for one requested object.
	DeletedObjectResult = core.DeletedObjectResult
	// DeletionPlan is the predicted result of deleting the selected marked objects.
	DeletionPlan = core.DeletionPlan
	// DocumentService is the canonical interface for document service operations.
	// Handlers and decorators depend on this interface — not on concrete service types.
	//
	// Decorators wrap DocumentService to add cross-cutting concerns
	// (logging, metrics, tracing, caching) without modifying business logic.
	DocumentService[T any] = core.DocumentService[T]
	// DomainEvent represents an event to be published via outbox.
	DomainEvent = core.DomainEvent
	// FindReferencesRequest specifies which entity to search references for.
	FindReferencesRequest = core.FindReferencesRequest
	// Hook is a function that runs at specific lifecycle points.
	Hook[T any] = core.Hook[T]
	// HookRegistry stores lifecycle hooks for an entity type.
	// Hooks are executed in priority order (lower = first).
	// When multiple extensions register hooks for the same event,
	// priority ensures deterministic execution order.
	HookRegistry[T any] = core.HookRegistry[T]
	// ListFilter contains common filtering options for list operations.
	// Uses cursor-based (keyset) pagination — no Offset.
	ListFilter = core.ListFilter
	// MarkedObject is one deletion-marked object together with what references it.
	MarkedObject = core.MarkedObject
	// MarkedObjectRef identifies one object by its registry entity name and ID.
	MarkedObjectRef = core.MarkedObjectRef
	// OutboxPublisher writes events to the transactional outbox.
	// Handled by the infrastructure layer (e.g. postgres.OutboxPublisher).
	OutboxPublisher = core.OutboxPublisher
	// RefResolver resolves typed references into human-readable presentations.
	// Uses metadata registry to determine entity type and table, then queries DB.
	RefResolver = core.RefResolver
	// RelatedDocFinder searches for all documents that reference the given entity.
	// Uses RefFinder + metadata.Registry to compute reverse document links.
	RelatedDocFinder = core.RelatedDocFinder
)

const (
	// BlockLiveReference: an object without a deletion mark references it.
	BlockLiveReference = core.BlockLiveReference
	// BlockNotSelected: a marked object that is not selected references it.
	BlockNotSelected = core.BlockNotSelected
	// DeletionBlocked: something that stays in the database references it.
	DeletionBlocked = core.DeletionBlocked
	// DeletionExcluded: the object is marked but was not selected.
	DeletionExcluded       = core.DeletionExcluded
	DeletionOutcomeFailed  = core.DeletionOutcomeFailed
	DeletionOutcomeSkipped = core.DeletionOutcomeSkipped
	// DeletionReady: nothing references the object.
	DeletionReady = core.DeletionReady
	// DeletionWithGroup: only objects deleted in the same plan reference it.
	DeletionWithGroup = core.DeletionWithGroup
	// SourceKindSystem is a table outside the metadata registry that holds a
	// foreign key to the object. Such rows can never be marked for deletion.
	SourceKindSystem = core.SourceKindSystem
	// Mandatory hooks execute after the write and before the outer commit.
	// Failures roll back the mutation. External work is enqueued to an outbox here.
	TransactionCreate = core.TransactionCreate
)

// Chain composes multiple middlewares into a single middleware.
// Middlewares are applied inside-out: the first middleware in the list is the outermost wrapper.
//
// Usage:
//
//	decorated := domain.Chain[*CryptoInvoice](
//	    domain.WithLogging[*CryptoInvoice]("crypto-invoice"),
//	    // future: domain.WithMetrics[*CryptoInvoice]("crypto-invoice"),
//	)(concreteService)
func Chain[T any](middlewares ...core.ServiceMiddleware[T]) core.ServiceMiddleware[T] {
	return core.Chain[T](middlewares...)
}

// NewBaseDocumentService creates a new BaseDocumentService.
func NewBaseDocumentService[T core.DocumentEntity[L], L any](cfg core.BaseDocumentServiceConfig[T, L]) *core.BaseDocumentService[T, L] {
	return core.NewBaseDocumentService[T, L](cfg)
}

// NewBaseHeaderDocumentService creates a new BaseHeaderDocumentService.
func NewBaseHeaderDocumentService[T core.HeaderDocumentEntity](cfg core.BaseHeaderDocumentServiceConfig[T]) *core.BaseHeaderDocumentService[T] {
	return core.NewBaseHeaderDocumentService[T](cfg)
}

// NewCatalogService creates a new catalog service.
// Identity and hierarchy are read from the entity definition.
func NewCatalogService[T entity.CatalogEntity](cfg core.CatalogServiceConfig[T]) *core.CatalogService[T] {
	return core.NewCatalogService[T](cfg)
}

// NewEventLogCatalogService records successful mutations in the same transaction.
// Writer errors are mandatory failures; the service rolls back the mutation.
func NewEventLogCatalogService[T entity.CatalogEntity](svc *core.CatalogService[T], entityName string, writer eventlog.Writer) {
	core.NewEventLogCatalogService[T](svc, entityName, writer)
}

// WithEventLog returns a ServiceMiddleware that records events to the event log.
// If writer is nil, returns the service unchanged (no-op).
func WithEventLog[T any](entityName string, writer eventlog.Writer) core.ServiceMiddleware[T] {
	return core.WithEventLog[T](entityName, writer)
}

// WithLogging returns a ServiceMiddleware that wraps a DocumentService with logging.
func WithLogging[T any](entityName string) core.ServiceMiddleware[T] {
	return core.WithLogging[T](entityName)
}

// WithOutboxEvents returns a ServiceMiddleware that records successfully completed
// business operations to the transactional outbox system for automation.
func WithOutboxEvents[T any](entityName string, publisher core.OutboxPublisher, currencyResolver core.CurrencyMetadataResolver) core.ServiceMiddleware[T] {
	return core.WithOutboxEvents[T](entityName, publisher, currencyResolver)
}
