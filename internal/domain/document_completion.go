package domain

import (
	"context"
	"metapus/internal/core/apperror"
	"metapus/internal/core/security"
	"metapus/internal/core/tx"
	"metapus/pkg/logger"
)

// runDocumentMutation gives Before and Transaction hooks the same rollback
// boundary, including when a caller catches an error inside an outer command.
func runDocumentMutation(ctx context.Context, manager func(context.Context) (tx.Manager, error), fn func(context.Context) error) error {
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	txm, err := manager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	if nested, ok := txm.(tx.SavepointManager); ok {
		return nested.RunInSavepoint(ctx, fn)
	}
	return txm.RunInTransaction(ctx, fn)
}

func completeDocumentMutation[T any](ctx context.Context, hooks *HookRegistry[T], entity T, phase, after HookEvent) error {
	if err := hooks.Run(ctx, phase, entity); err != nil {
		return err
	}
	if !hooks.Has(after) {
		return nil
	}
	snapshot := security.ProjectForRead(entity, nil)
	return tx.OnCommit(ctx, func(ctx context.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error(ctx, "document after-commit hook panicked", "hook", after)
			}
		}()
		if err := hooks.Run(ctx, after, snapshot); err != nil {
			logger.Warn(ctx, "document after-commit hook failed", "hook", after, "error", err)
		}
	})
}
