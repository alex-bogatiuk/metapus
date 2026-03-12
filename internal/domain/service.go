// Package domain provides core business logic interfaces and types.
package domain

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/core/tenant"
	"metapus/internal/core/tx"
	"metapus/pkg/logger"
)

// CatalogService provides business logic for catalog entities.
// In Database-per-Tenant architecture, TxManager can be nil - it will be obtained from context.
type CatalogService[T entity.Validatable] struct {
	repo         CatalogRepository[T]
	txManager    tx.Manager // Optional - if nil, obtained from context
	numerator    numerator.Generator
	policyEngine *security.PolicyEngine
	hooks        *HookRegistry[T]

	// entityName for error messages and numerator prefix
	entityName string

	// meta contains hierarchy and other metadata configuration
	meta entity.CatalogMeta

	// hierarchyValidator validates hierarchy constraints (nil for flat catalogs)
	hierarchyValidator *HierarchyValidator
}

// CatalogServiceConfig configures the catalog service.
type CatalogServiceConfig[T entity.Validatable] struct {
	Repo         CatalogRepository[T]
	TxManager    tx.Manager // Optional for Database-per-Tenant
	Numerator    numerator.Generator
	PolicyEngine *security.PolicyEngine // Optional — CEL policy evaluation
	EntityName   string
}

// NewCatalogService creates a new catalog service.
// CatalogMeta is automatically resolved from the entity registry.
func NewCatalogService[T entity.Validatable](cfg CatalogServiceConfig[T]) *CatalogService[T] {
	meta := entity.GetCatalogMeta(cfg.EntityName)

	var hv *HierarchyValidator
	if meta.Hierarchical {
		hv = NewHierarchyValidator(meta)
	}

	return &CatalogService[T]{
		repo:               cfg.Repo,
		txManager:          cfg.TxManager,
		numerator:          cfg.Numerator,
		policyEngine:       cfg.PolicyEngine,
		hooks:              NewHookRegistry[T](),
		entityName:         cfg.EntityName,
		meta:               meta,
		hierarchyValidator: hv,
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

// SetPolicyEngine sets the CEL policy engine after construction.
func (s *CatalogService[T]) SetPolicyEngine(engine *security.PolicyEngine) {
	s.policyEngine = engine
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
		return apperror.NewNotFound(s.entityName, idOrCode).WithCause(err)
	}
	if apperror.IsAppError(err) {
		return err
	}
	// Internal errors already have cause set via NewInternal(err)
	return apperror.NewInternal(err).WithDetail("entity", s.entityName).WithDetail("id", idOrCode)
}

// checkCELPolicy evaluates CEL policy rules for the given action and entity.
// Returns nil if no PolicyEngine is configured or no rules match.
func (s *CatalogService[T]) checkCELPolicy(ctx context.Context, action string, ent T) error {
	if s.policyEngine == nil {
		return nil
	}
	rules := security.GetApplicableRules(ctx, s.entityName, action)
	if len(rules) == 0 {
		return nil
	}
	return s.policyEngine.Evaluate(ctx, rules, action, ent)
}

// checkRLSAccess checks if the current DataScope allows access to the entity.
// Uses type assertion — only entities implementing RLSDimensionable are checked.
func (s *CatalogService[T]) checkRLSAccess(ctx context.Context, ent T) error {
	scope := security.GetDataScope(ctx)
	if scope == nil || scope.IsAdmin {
		return nil
	}
	if dimensionable, ok := any(ent).(security.RLSDimensionable); ok {
		if !scope.CanAccessRecord(dimensionable.GetRLSDimensions()) {
			return apperror.NewForbidden("access denied by security policy")
		}
	}
	return nil
}

// Create creates a new catalog entity.
func (s *CatalogService[T]) Create(ctx context.Context, entity T) error {
	// RLS: check write permission
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	if err := s.checkRLSAccess(ctx, entity); err != nil {
		return err
	}

	// 1. Validate entity invariants
	if err := entity.Validate(ctx); err != nil {
		return s.normalizeValidationErr(err)
	}

	// CEL policy check
	if err := s.checkCELPolicy(ctx, "create", entity); err != nil {
		return err
	}

	// 2. Validate hierarchy constraints (if hierarchical catalog)
	if err := s.validateHierarchy(ctx, entity); err != nil {
		return err
	}

	// 3. Run before-create hooks
	if err := s.hooks.RunBeforeCreate(ctx, entity); err != nil {
		return err
	}

	// 4. Create in transaction
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

	// 5. Run after-create hooks (outside transaction)
	if err := s.hooks.RunAfterCreate(ctx, entity); err != nil {
		logger.Warn(ctx, "after-create hook failed", "entity", s.entityName, "error", err)
	}

	return nil
}

// GetByID retrieves entity by ID.
func (s *CatalogService[T]) GetByID(ctx context.Context, entityID id.ID) (T, error) {
	entity, err := s.repo.GetByID(ctx, entityID)
	if err != nil {
		return entity, s.normalizeGetErr(err, entityID.String())
	}
	// RLS: point-check after fetch
	if err := s.checkRLSAccess(ctx, entity); err != nil {
		var zero T
		return zero, err
	}
	// CEL policy check for read
	if err := s.checkCELPolicy(ctx, "read", entity); err != nil {
		var zero T
		return zero, err
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
	// RLS: check write permission and record access
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	if err := s.checkRLSAccess(ctx, entity); err != nil {
		return err
	}

	// Fetch existing entity for CEL evaluation and FLS validation.
	// CEL rules must evaluate against the existing state,
	// not the input entity where fields default to zero values.
	oldEntity, err := s.repo.GetByID(ctx, any(entity).(interface{ GetID() id.ID }).GetID())
	if err != nil {
		return fmt.Errorf("fetch existing entity: %w", err)
	}

	// CEL policy check — evaluate against existing entity state
	if err := s.checkCELPolicy(ctx, "update", oldEntity); err != nil {
		return err
	}

	// FLS: validate that no restricted fields were modified
	if writePolicy := security.GetFieldPolicy(ctx, s.entityName, "write"); writePolicy != nil {
		if err := security.NewFieldMasker().ValidateWrite(oldEntity, entity, writePolicy); err != nil {
			return err
		}
	}

	// 1. Validate entity invariants
	if err := entity.Validate(ctx); err != nil {
		return s.normalizeValidationErr(err)
	}

	// 2. Validate hierarchy constraints (if hierarchical catalog)
	if err := s.validateHierarchy(ctx, entity); err != nil {
		return err
	}

	// 3. Run before-update hooks
	if err := s.hooks.RunBeforeUpdate(ctx, entity); err != nil {
		return err
	}

	// 4. Update in transaction
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

	// 5. Run after-update hooks
	if err := s.hooks.RunAfterUpdate(ctx, entity); err != nil {
		logger.Warn(ctx, "after-update hook failed", "entity", s.entityName, "error", err)
	}

	return nil
}

// Delete performs soft delete.
func (s *CatalogService[T]) Delete(ctx context.Context, entityID id.ID) error {
	// RLS: check write permission
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}

	// 1. Get entity first (for hooks + RLS point-check)
	entity, err := s.repo.GetByID(ctx, entityID)
	if err != nil {
		return s.normalizeGetErr(err, entityID.String())
	}
	if err := s.checkRLSAccess(ctx, entity); err != nil {
		return err
	}

	// CEL policy check
	if err := s.checkCELPolicy(ctx, "delete", entity); err != nil {
		return err
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
		if err := s.repo.Delete(ctx, entityID); err != nil {
			return fmt.Errorf("delete %s: %w", s.entityName, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 4. Run after-delete hooks
	if err := s.hooks.RunAfterDelete(ctx, entity); err != nil {
		logger.Warn(ctx, "after-delete hook failed", "entity", s.entityName, "error", err)
	}

	return nil
}

func (s *CatalogService[T]) SetDeletionMark(ctx context.Context, entityID id.ID, marked bool) error {
	// RLS: check write permission
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	return s.repo.SetDeletionMark(ctx, entityID, marked)
}

// List retrieves entities with cursor-based pagination.
// Defense-in-depth: if DataScope was not injected by handler (ParseListFilter),
// fall back to context — ensures RLS is enforced even for direct service calls.
func (s *CatalogService[T]) List(ctx context.Context, filter ListFilter) (CursorListResult[T], error) {
	if filter.DataScope == nil {
		filter.DataScope = security.GetDataScope(ctx)
	}
	return s.repo.List(ctx, filter)
}

// Exists checks if entity exists.
func (s *CatalogService[T]) Exists(ctx context.Context, entityID id.ID) (bool, error) {
	return s.repo.Exists(ctx, entityID)
}

// GetTree retrieves hierarchical structure.
// Returns error for flat (non-hierarchical) catalogs.
func (s *CatalogService[T]) GetTree(ctx context.Context, rootID *id.ID) ([]T, error) {
	if !s.meta.Hierarchical {
		return nil, apperror.NewValidation(
			fmt.Sprintf("%s is a flat catalog and does not support hierarchy", s.entityName),
		)
	}
	return s.repo.GetTree(ctx, rootID)
}

// GetPath retrieves the path from root to entity.
// Returns error for flat (non-hierarchical) catalogs.
func (s *CatalogService[T]) GetPath(ctx context.Context, entityID id.ID) ([]T, error) {
	if !s.meta.Hierarchical {
		return nil, apperror.NewValidation(
			fmt.Sprintf("%s is a flat catalog and does not support hierarchy", s.entityName),
		)
	}
	return s.repo.GetPath(ctx, entityID)
}

// Meta returns the catalog metadata configuration.
func (s *CatalogService[T]) Meta() entity.CatalogMeta {
	return s.meta
}

// validateHierarchy checks hierarchy constraints if the entity implements ParentAccessor.
func (s *CatalogService[T]) validateHierarchy(ctx context.Context, ent T) error {
	if s.hierarchyValidator == nil {
		return nil
	}

	accessor, ok := any(ent).(ParentAccessor)
	if !ok {
		// Entity doesn't implement ParentAccessor — skip hierarchy validation
		return nil
	}

	// Adapter: bridge repo.GetByID to ParentAccessor
	getByID := func(ctx context.Context, entID id.ID) (ParentAccessor, error) {
		result, err := s.repo.GetByID(ctx, entID)
		if err != nil {
			return nil, err
		}
		pa, ok := any(result).(ParentAccessor)
		if !ok {
			return nil, fmt.Errorf("entity does not implement ParentAccessor")
		}
		return pa, nil
	}

	return s.hierarchyValidator.ValidateHierarchy(ctx, accessor, getByID)
}
