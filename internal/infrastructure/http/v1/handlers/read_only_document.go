package handlers

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/domain"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/tenantdb"
)

// ReadOnlyDocumentPresenter maps a system-owned document to its response DTO.
// Collect registers the references the DTO displays; Present builds the DTO.
type ReadOnlyDocumentPresenter[T any, R any] struct {
	Collect func(*postgres.ReferenceResolver, T)
	Present func(T, postgres.ResolvedRefs) R
}

// NewReadOnlyDocumentHandler serves system-owned documents that are created
// and decided only through their own workflows: list, card and register
// movements. Create and update routes are not mounted for them.
func NewReadOnlyDocumentHandler[T any, R any](base *BaseHandler, service domain.DocumentService[T], entityName string,
	presenter ReadOnlyDocumentPresenter[T, R],
	movementProviders []entity.MovementProvider, movementRefResolver domain.RefResolver, settingsRepo settings.Repository,
) *BaseDocumentHandler[T, struct{}, struct{}] {
	if presenter.Collect == nil || presenter.Present == nil {
		panic("read-only document " + entityName + " requires a presenter")
	}
	refuse := apperror.NewForbidden("document is created and decided only by its own workflow")
	return NewBaseDocumentHandler(base, BaseDocumentHandlerConfig[T, struct{}, struct{}]{
		Service: service, EntityName: entityName,
		MapCreateDTO: func(struct{}) (T, error) { var zero T; return zero, refuse },
		MapUpdateDTO: func(struct{}, T) (T, error) { var zero T; return zero, refuse },
		MapToDTO:     func(doc T) any { return presenter.Present(doc, nil) },
		ResolveRefs: func(ctx context.Context, docs ...T) (any, error) {
			resolver := postgres.NewReferenceResolver()
			for _, doc := range docs {
				presenter.Collect(resolver, doc)
			}
			return resolver.Resolve(ctx, tenantdb.MustGetPool(ctx))
		},
		MapToDTOWithRefs: func(doc T, refs any) any {
			resolved, _ := refs.(postgres.ResolvedRefs)
			return presenter.Present(doc, resolved)
		},
		MovementProviders: movementProviders, MovementRefResolver: movementRefResolver, SettingsRepo: settingsRepo,
	})
}
