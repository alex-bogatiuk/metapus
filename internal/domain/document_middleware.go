package domain

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/pkg/logger"
)

// DocumentService is the canonical interface for document service operations.
// Handlers and decorators depend on this interface — not on concrete service types.
//
// Decorators wrap DocumentService to add cross-cutting concerns
// (logging, metrics, tracing, caching) without modifying business logic.
type DocumentService[T any] interface {
	Create(ctx context.Context, entity T) error
	GetByID(ctx context.Context, id id.ID) (T, error)
	Update(ctx context.Context, entity T) error
	Delete(ctx context.Context, id id.ID) error
	Post(ctx context.Context, id id.ID) error
	Unpost(ctx context.Context, id id.ID) error
	PostAndSave(ctx context.Context, entity T) error
	UpdateAndRepost(ctx context.Context, entity T) error
	SetDeletionMark(ctx context.Context, id id.ID, marked bool) error
	List(ctx context.Context, filter ListFilter) (ListResult[T], error)
}

// ServiceMiddleware is a function that wraps a DocumentService with additional behaviour.
// Multiple middlewares can be composed via Chain.
type ServiceMiddleware[T any] func(next DocumentService[T]) DocumentService[T]

// Chain composes multiple middlewares into a single middleware.
// Middlewares are applied inside-out: the first middleware in the list is the outermost wrapper.
//
// Usage:
//
//	decorated := domain.Chain[*GoodsReceipt](
//	    domain.WithLogging[*GoodsReceipt]("goods-receipt"),
//	    // future: domain.WithMetrics[*GoodsReceipt]("goods-receipt"),
//	)(concreteService)
func Chain[T any](middlewares ...ServiceMiddleware[T]) ServiceMiddleware[T] {
	return func(next DocumentService[T]) DocumentService[T] {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

// ---------------------------------------------------------------------------
// Logging decorator
// ---------------------------------------------------------------------------

// LoggingDocumentService is a Decorator that logs every service method call
// with structured fields: method name, duration, error (if any).
type LoggingDocumentService[T any] struct {
	next       DocumentService[T]
	entityName string
}

// WithLogging returns a ServiceMiddleware that wraps a DocumentService with logging.
func WithLogging[T any](entityName string) ServiceMiddleware[T] {
	return func(next DocumentService[T]) DocumentService[T] {
		return &LoggingDocumentService[T]{next: next, entityName: entityName}
	}
}

func (s *LoggingDocumentService[T]) log(ctx context.Context, method string, start time.Time, err error) {
	duration := time.Since(start)
	if err != nil {
		logger.Error(ctx, s.entityName+"."+method+" failed",
			"method", method,
			"entity", s.entityName,
			"duration", duration,
			"error", err,
		)
		return
	}
	logger.Info(ctx, s.entityName+"."+method,
		"method", method,
		"entity", s.entityName,
		"duration", duration,
	)
}

func (s *LoggingDocumentService[T]) Create(ctx context.Context, entity T) (err error) {
	defer func(start time.Time) { s.log(ctx, "Create", start, err) }(time.Now())
	return s.next.Create(ctx, entity)
}

func (s *LoggingDocumentService[T]) GetByID(ctx context.Context, docID id.ID) (result T, err error) {
	defer func(start time.Time) { s.log(ctx, "GetByID", start, err) }(time.Now())
	return s.next.GetByID(ctx, docID)
}

func (s *LoggingDocumentService[T]) Update(ctx context.Context, entity T) (err error) {
	defer func(start time.Time) { s.log(ctx, "Update", start, err) }(time.Now())
	return s.next.Update(ctx, entity)
}

func (s *LoggingDocumentService[T]) Delete(ctx context.Context, docID id.ID) (err error) {
	defer func(start time.Time) { s.log(ctx, "Delete", start, err) }(time.Now())
	return s.next.Delete(ctx, docID)
}

func (s *LoggingDocumentService[T]) Post(ctx context.Context, docID id.ID) (err error) {
	defer func(start time.Time) { s.log(ctx, "Post", start, err) }(time.Now())
	return s.next.Post(ctx, docID)
}

func (s *LoggingDocumentService[T]) Unpost(ctx context.Context, docID id.ID) (err error) {
	defer func(start time.Time) { s.log(ctx, "Unpost", start, err) }(time.Now())
	return s.next.Unpost(ctx, docID)
}

func (s *LoggingDocumentService[T]) PostAndSave(ctx context.Context, entity T) (err error) {
	defer func(start time.Time) { s.log(ctx, "PostAndSave", start, err) }(time.Now())
	return s.next.PostAndSave(ctx, entity)
}

func (s *LoggingDocumentService[T]) UpdateAndRepost(ctx context.Context, entity T) (err error) {
	defer func(start time.Time) { s.log(ctx, "UpdateAndRepost", start, err) }(time.Now())
	return s.next.UpdateAndRepost(ctx, entity)
}

func (s *LoggingDocumentService[T]) SetDeletionMark(ctx context.Context, docID id.ID, marked bool) (err error) {
	defer func(start time.Time) { s.log(ctx, "SetDeletionMark", start, err) }(time.Now())
	return s.next.SetDeletionMark(ctx, docID, marked)
}

func (s *LoggingDocumentService[T]) List(ctx context.Context, filter ListFilter) (result ListResult[T], err error) {
	defer func(start time.Time) { s.log(ctx, "List", start, err) }(time.Now())
	return s.next.List(ctx, filter)
}
