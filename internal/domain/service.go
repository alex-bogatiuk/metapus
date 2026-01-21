// Package domain provides core business logic interfaces and types.
package domain

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/tx"
	"metapus/pkg/numerator"
)

// CatalogService provides business logic for catalog entities.
// In Database-per-Tenant architecture, TxManager can be nil - it will be obtained from context.
type CatalogService[T entity.Validatable] struct {
	repo      CatalogRepository[T]
	txManager tx.Manager // Optional - if nil, obtained from context
	numerator *numerator.Service
	hooks     *HookRegistry[T]

	// entityName for error messages and numerator prefix
	entityName string
}

// CatalogServiceConfig configures the catalog service.
type CatalogServiceConfig[T entity.Validatable] struct {
	Repo       CatalogRepository[T]
	TxManager  tx.Manager // Optional for Database-per-Tenant
	Numerator  *numerator.Service
	EntityName string
}

// NewCatalogService creates a new catalog service.
func NewCatalogService[T entity.Validatable](cfg CatalogServiceConfig[T]) *CatalogService[T] {
	return &CatalogService[T]{
		repo:       cfg.Repo,
		txManager:  cfg.TxManager,
		numerator:  cfg.Numerator,
		hooks:      NewHookRegistry[T](),
		entityName: cfg.EntityName,
	}
}

// getTxManager returns TxManager from config or context.
func (s *CatalogService[T]) getTxManager(ctx context.Context) (tx.Manager, error) {
	if s.txManager != nil {
		return s.txManager, nil
	}
	// Get from context (Database-per-Tenant mode)
	return tenant.GetTxManager(ctx)
}

// Hooks returns the hook registry for external registration.
func (s *CatalogService[T]) Hooks() *HookRegistry[T] {
	return s.hooks
}

func (s *CatalogService[T]) normalizeValidationErr(err error) error {
	if err == nil {
		return nil
	}
	// If entity already returns structured AppError, keep it.
	if apperror.IsAppError(err) {
		return err
	}
	return apperror.NewValidation(err.Error())
}

func (s *CatalogService[T]) normalizeGetErr(err error, idOrCode any) error {
	if err == nil {
		return nil
	}
	// Preserve existing AppError, but ensure not-found is mapped to the correct entity name.
	if apperror.IsNotFound(err) {
		return apperror.NewNotFound(s.entityName, idOrCode)
	}
	if apperror.IsAppError(err) {
		return err
	}
	return apperror.NewInternal(err).WithDetail("entity", s.entityName).WithDetail("id", idOrCode)
}

// Create creates a new catalog entity.
func (s *CatalogService[T]) Create(ctx context.Context, entity T) error {
	// 1. Validate entity invariants
	if err := entity.Validate(ctx); err != nil {
		return s.normalizeValidationErr(err)
	}

	// 2. Run before-create hooks
	if err := s.hooks.RunBeforeCreate(ctx, entity); err != nil {
		return err
	}

	// 3. Create in transaction
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, entity); err != nil {
			return fmt.Errorf("create %s: %w", s.entityName, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 4. Run after-create hooks (outside transaction)
	if err := s.hooks.RunAfterCreate(ctx, entity); err != nil {
		// Log but don't fail - entity is already created
		// logger.Warn(ctx, "after-create hook failed", "error", err)
	}

	return nil
}

// GetByID retrieves entity by ID.
func (s *CatalogService[T]) GetByID(ctx context.Context, entityID id.ID) (T, error) {
	entity, err := s.repo.GetByID(ctx, entityID)
	if err != nil {
		return entity, s.normalizeGetErr(err, entityID.String())
	}
	return entity, nil
}

// GetByCode retrieves entity by code.
func (s *CatalogService[T]) GetByCode(ctx context.Context, code string) (T, error) {
	entity, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return entity, s.normalizeGetErr(err, code)
	}
	return entity, nil
}

// Update updates an existing entity.
func (s *CatalogService[T]) Update(ctx context.Context, entity T) error {
	// 1. Validate entity invariants
	if err := entity.Validate(ctx); err != nil {
		return s.normalizeValidationErr(err)
	}

	// 2. Run before-update hooks
	if err := s.hooks.RunBeforeUpdate(ctx, entity); err != nil {
		return err
	}

	// 3. Update in transaction
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Update(ctx, entity); err != nil {
			return fmt.Errorf("update %s: %w", s.entityName, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 4. Run after-update hooks
	if err := s.hooks.RunAfterUpdate(ctx, entity); err != nil {
		// Log but don't fail
	}

	return nil
}

// Delete performs soft delete.
func (s *CatalogService[T]) Delete(ctx context.Context, entityID id.ID) error {
	// 1. Get entity first (for hooks)
	entity, err := s.repo.GetByID(ctx, entityID)
	if err != nil {
		return s.normalizeGetErr(err, entityID.String())
	}

	// 2. Run before-delete hooks
	if err := s.hooks.RunBeforeDelete(ctx, entity); err != nil {
		return err
	}

	// 3. Soft delete in transaction
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.SetDeletionMark(ctx, entityID, true); err != nil {
			return fmt.Errorf("delete %s: %w", s.entityName, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 4. Run after-delete hooks
	if err := s.hooks.RunAfterDelete(ctx, entity); err != nil {
		// Log but don't fail
	}

	return nil
}

func (s *CatalogService[T]) SetDeletionMark(ctx context.Context, entityID id.ID, marked bool) error {
	return s.repo.SetDeletionMark(ctx, entityID, marked)
}

// List retrieves entities with filtering.
func (s *CatalogService[T]) List(ctx context.Context, filter ListFilter) (ListResult[T], error) {
	return s.repo.List(ctx, filter)
}

// Exists checks if entity exists.
func (s *CatalogService[T]) Exists(ctx context.Context, entityID id.ID) (bool, error) {
	return s.repo.Exists(ctx, entityID)
}

// GetTree retrieves hierarchical structure.
func (s *CatalogService[T]) GetTree(ctx context.Context, rootID *id.ID) ([]T, error) {
	return s.repo.GetTree(ctx, rootID)
}
