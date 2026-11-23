package handlers

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/application/command"
	"github.com/alex-bogatiuk/metapus/internal/application/readaccess"
	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/jsoncodec"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/middleware"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/jackc/pgx/v5"
)

// present is the single projection boundary for catalog responses. FK collection
// happens after masking so a hidden source field cannot reveal a related name.
func (h *CatalogHandler[T, CreateDTO, UpdateDTO]) present(ctx context.Context, entities ...T) ([]any, []T, error) {
	reads, ok := readaccess.FromContext(ctx)
	if !ok {
		return nil, nil, apperror.NewForbidden("authorized reader is unavailable")
	}
	if _, err := reads.Require(ctx, h.entityName); err != nil {
		return nil, nil, err
	}
	policy := security.GetFieldPolicy(ctx, h.entityName, "read")
	projections := make([]T, len(entities))
	for i, item := range entities {
		if err := h.service.AuthorizeRead(ctx, item); err != nil {
			return nil, nil, err
		}
		projections[i] = security.ProjectForRead(item, policy)
	}
	var refs any
	if h.resolveRefs != nil {
		var err error
		refs, err = h.resolveRefs(ctx, projections...)
		if err != nil {
			return nil, nil, err
		}
	}
	responses := make([]any, len(projections))
	for i, item := range projections {
		responses[i] = h.ToDTO(item, refs)
	}
	return responses, projections, nil
}

// ReplayPresenter returns a freshly authorized representation of the original
// resource. A durable command is never executed again to refresh its response.
func (h *CatalogHandler[T, CreateDTO, UpdateDTO]) ReplayPresenter() middleware.ReplayPresenter {
	return func(ctx context.Context, result command.Result) (command.Result, error) {
		reads, ok := readaccess.FromContext(ctx)
		if !ok {
			return command.Result{}, apperror.NewForbidden("authorized reader is unavailable")
		}
		if _, err := reads.Require(ctx, h.entityName); err != nil {
			return command.Result{}, err
		}
		uid, err := id.Parse(result.ResourceID)
		if err != nil || id.IsNil(uid) {
			return command.Result{}, apperror.NewForbidden("command resource cannot be authorized")
		}
		manager := postgres.MustGetTxManager(ctx)
		options := postgres.DefaultTxOptions()
		options.IsolationLevel = pgx.RepeatableRead
		err = manager.RunInTransactionWithOptions(ctx, options, func(ctx context.Context) error {
			item, err := h.service.GetByID(ctx, uid)
			if err != nil {
				return err
			}
			responses, _, err := h.present(ctx, item)
			if err != nil {
				return err
			}
			result.Payload, err = jsoncodec.Marshal(responses[0])
			return err
		})
		return result, err
	}
}
