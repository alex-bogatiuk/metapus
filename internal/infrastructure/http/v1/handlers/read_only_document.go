package handlers

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/domain"
	"metapus/internal/domain/settings"
)

// NewReadOnlyDocumentHandler serves system-owned documents that are created
// and decided only through their own workflows: list, card and register
// movements. Create and update routes are not mounted for them.
func NewReadOnlyDocumentHandler[T any](base *BaseHandler, service domain.DocumentService[T], entityName string,
	movementProviders []entity.MovementProvider, movementRefResolver domain.RefResolver, settingsRepo settings.Repository,
) *BaseDocumentHandler[T, struct{}, struct{}] {
	refuse := apperror.NewForbidden("document is created and decided only by its own workflow")
	return NewBaseDocumentHandler(base, BaseDocumentHandlerConfig[T, struct{}, struct{}]{
		Service: service, EntityName: entityName,
		MapCreateDTO: func(struct{}) (T, error) { var zero T; return zero, refuse },
		MapUpdateDTO: func(struct{}, T) (T, error) { var zero T; return zero, refuse },
		MapToDTO:     func(doc T) any { return doc },
		ResolveRefs:  func(context.Context, ...T) (any, error) { return nil, nil },
		MapToDTOWithRefs: func(doc T, _ any) any {
			return doc
		},
		MovementProviders: movementProviders, MovementRefResolver: movementRefResolver, SettingsRepo: settingsRepo,
	})
}
