// Package domain provides core business logic interfaces and types.
package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

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
type CatalogService[T entity.CatalogEntity] struct {
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

	// codeSource and codeSeries are the Definition's rule for the item code.
	codeSource entity.CodeSource
	codeSeries numerator.Config
}

// CatalogServiceConfig configures the catalog service.
type CatalogServiceConfig[T entity.CatalogEntity] struct {
	// Definition supplies identity and hierarchy for every catalog.
	// Both persistence and service use this definition directly.
	Definition   *entity.Definition
	Repo         CatalogRepository[T]
	TxManager    tx.Manager // Optional for Database-per-Tenant
	Numerator    numerator.Generator
	PolicyEngine *security.PolicyEngine // Optional — CEL policy evaluation
}

// NewCatalogService creates a new catalog service.
// Identity and hierarchy are read from the entity definition.
func NewCatalogService[T entity.CatalogEntity](cfg CatalogServiceConfig[T]) *CatalogService[T] {
	if cfg.Definition == nil {
		panic("catalog service requires an entity definition")
	}
	if err := cfg.Definition.Validate(); err != nil {
		panic(err)
	}
	if cfg.Definition.Kind != entity.CatalogKind {
		panic("catalog service requires a catalog definition")
	}
	meta := cfg.Definition.Hierarchy

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
		entityName:         cfg.Definition.Key,
		meta:               meta,
		hierarchyValidator: hv,
		codeSource:         cfg.Definition.CodeSource(),
		codeSeries:         numerator.CatalogSeries(cfg.Definition.CodeSeries()),
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
	return apperror.NewValidation("entity validation failed").WithCause(err)
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

// checkCELPolicy delegates to security.CheckCELPolicy.
func (s *CatalogService[T]) checkCELPolicy(ctx context.Context, action string, ent T) error {
	return security.CheckCELPolicy(ctx, s.policyEngine, s.entityName, action, ent)
}

// checkRLSAccess delegates to security.CheckRLSAccess.
func (s *CatalogService[T]) checkRLSAccess(ctx context.Context, ent T) error {
	return security.CheckRLSAccess(ctx, s.entityName, ent)
}

// AuthorizeRead checks a complete, already loaded entity before presentation,
// including responses produced by a mutation.
func (s *CatalogService[T]) AuthorizeRead(ctx context.Context, ent T) error {
	if err := s.checkRLSAccess(ctx, ent); err != nil {
		return err
	}
	return s.checkCELPolicy(ctx, "read", ent)
}

// Create creates a new catalog entity.
func (s *CatalogService[T]) Create(ctx context.Context, ent T) error {
	normalizeCatalog(ent)
	// Pure input validation needs no database and remains a validation error
	// even when persistence is unavailable. Transactional checks run below.
	if err := ent.Validate(ctx); err != nil {
		return s.normalizeValidationErr(err)
	}
	return s.mutate(ctx, func(ctx context.Context) error {
		if err := s.checkRLSAccess(ctx, ent); err != nil {
			return err
		}
		if err := ent.Validate(ctx); err != nil {
			return s.normalizeValidationErr(err)
		}
		if err := s.checkCELPolicy(ctx, "create", ent); err != nil {
			return err
		}
		if err := s.hooks.RunBeforeCreate(ctx, ent); err != nil {
			return err
		}
		if err := s.validateHierarchy(ctx, ent); err != nil {
			return err
		}
		if err := s.assignCode(ctx, ent, ""); err != nil {
			return err
		}
		if err := s.repo.Create(ctx, ent); err != nil {
			return fmt.Errorf("create %s: %w", s.entityName, err)
		}
		return s.completeMutation(ctx, ent, TransactionCreate, AfterCreate)
	})
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

// Update updates an existing entity.
func (s *CatalogService[T]) Update(ctx context.Context, ent T) error {
	return s.mutate(ctx, func(ctx context.Context) error {
		if err := s.checkRLSAccess(ctx, ent); err != nil {
			return err
		}
		current, err := s.repo.GetByID(ctx, ent.GetID())
		if err != nil {
			return s.normalizeGetErr(err, ent.GetID())
		}
		if err := s.checkRLSAccess(ctx, current); err != nil {
			return err
		}
		if err := s.checkCELPolicy(ctx, "update", current); err != nil {
			return err
		}
		if policy := security.GetFieldPolicy(ctx, s.entityName, "write"); policy != nil {
			if err := security.ValidateWrite(current, ent, policy); err != nil {
				return err
			}
		}
		normalizeCatalog(ent)
		if err := ent.Validate(ctx); err != nil {
			return s.normalizeValidationErr(err)
		}
		if err := s.hooks.RunBeforeUpdate(ctx, ent); err != nil {
			return err
		}
		if err := s.validateHierarchy(ctx, ent); err != nil {
			return err
		}
		if err := s.assignCode(ctx, ent, storedCode(current)); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, ent); err != nil {
			return fmt.Errorf("update %s: %w", s.entityName, err)
		}
		return s.completeMutation(ctx, ent, TransactionUpdate, AfterUpdate)
	})
}

// Delete physically removes a catalog entry after authorization and hooks.
func (s *CatalogService[T]) Delete(ctx context.Context, uid id.ID) error {
	return s.mutate(ctx, func(ctx context.Context) error {
		ent, err := s.authorizeDeletion(ctx, uid)
		if err != nil {
			return err
		}
		if err := s.hooks.RunBeforeDelete(ctx, ent); err != nil {
			return err
		}
		if err := s.repo.Delete(ctx, uid); err != nil {
			return fmt.Errorf("delete %s: %w", s.entityName, err)
		}
		return s.completeMutation(ctx, ent, TransactionDelete, AfterDelete)
	})
}

func (s *CatalogService[T]) SetDeletionMark(ctx context.Context, uid id.ID, marked bool) error {
	return s.mutate(ctx, func(ctx context.Context) error {
		ent, err := s.authorizeDeletion(ctx, uid)
		if err != nil {
			return err
		}
		if state, ok := any(ent).(interface{ IsDeletionMarked() bool }); ok && state.IsDeletionMarked() == marked {
			return nil
		}
		if err := s.repo.SetDeletionMark(ctx, uid, marked); err != nil {
			return err
		}
		// The event contains the version actually written by this transaction.
		ent, err = s.repo.GetByID(ctx, uid)
		if err != nil {
			return err
		}
		phase := TransactionClearDeletionMark
		if marked {
			phase = TransactionSetDeletionMark
		}
		return s.completeMutation(ctx, ent, phase, "")
	})
}

func (s *CatalogService[T]) authorizeDeletion(ctx context.Context, uid id.ID) (T, error) {
	ent, err := s.repo.GetByID(ctx, uid)
	if err != nil {
		return ent, s.normalizeGetErr(err, uid)
	}
	if err := s.checkRLSAccess(ctx, ent); err != nil {
		return ent, err
	}
	return ent, s.checkCELPolicy(ctx, "delete", ent)
}

func (s *CatalogService[T]) mutate(ctx context.Context, fn func(context.Context) error) error {
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	run := txm.RunInTransaction
	if nested, ok := txm.(tx.SavepointManager); ok {
		run = nested.RunInSavepoint
	}
	return run(ctx, func(ctx context.Context) error {
		if s.meta.Hierarchical {
			locker, ok := s.repo.(HierarchyLocker)
			if !ok {
				return apperror.NewInternal(fmt.Errorf("hierarchical repository has no locking port"))
			}
			if err := locker.LockHierarchy(ctx); err != nil {
				return err
			}
		}
		return fn(ctx)
	})
}

func (s *CatalogService[T]) completeMutation(ctx context.Context, ent T, phase, after HookEvent) error {
	if err := s.hooks.Run(ctx, phase, ent); err != nil {
		return err
	}
	if !s.hooks.Has(after) {
		return nil
	}
	// Later mutations/presentation must not change a queued callback's snapshot.
	snapshot := security.ProjectForRead(ent, nil)
	return tx.OnCommit(ctx, func(ctx context.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error(ctx, "catalog after-commit hook panicked", "entity", s.entityName, "hook", after)
			}
		}()
		if err := s.hooks.Run(ctx, after, snapshot); err != nil {
			logger.Warn(ctx, "catalog after-commit hook failed", "entity", s.entityName, "hook", after, "error", err)
		}
	})
}

// List retrieves entities with cursor-based pagination.
// Defense-in-depth: if DataScope was not injected by handler (ParseListFilter),
// fall back to context — ensures RLS is enforced even for direct service calls.
// After RLS filtering at DB level, applies CEL read rules to remove denied items.
//
// NOTE: When CEL read rules are active, TotalCount is approximate — it reflects
// the DB count minus items removed on THIS page only. Returned Items may be fewer
// than the requested limit. Clients should rely on HasMore/HasPrev for navigation.
func (s *CatalogService[T]) List(ctx context.Context, filter ListFilter) (CursorListResult[T], error) {
	if filter.DataScope == nil {
		filter.DataScope = security.GetDataScope(ctx)
	}
	result, err := s.repo.List(ctx, filter)
	if err != nil {
		return result, err
	}

	// Apply CEL read rules to filter out denied entities
	filtered, removed := security.FilterByReadPolicy(ctx, s.policyEngine, s.entityName, result.Items)
	result.Items = filtered
	if result.TotalCount != nil && removed > 0 {
		adjusted := *result.TotalCount - int64(removed)
		result.TotalCount = &adjusted
	}

	return result, nil
}

// Exists checks if entity exists.
func (s *CatalogService[T]) Exists(ctx context.Context, entityID id.ID) (bool, error) {
	return s.repo.Exists(ctx, entityID)
}

// GetTree retrieves hierarchical structure.
// Returns error for flat (non-hierarchical) catalogs.
// Applies RLS point-check and CEL read rules as post-filter on the result set.
func (s *CatalogService[T]) GetTree(ctx context.Context, rootID *id.ID) ([]T, error) {
	if !s.meta.Hierarchical {
		return nil, apperror.NewValidation(
			fmt.Sprintf("%s is a flat catalog and does not support hierarchy", s.entityName),
		)
	}
	items, err := s.repo.GetTree(ctx, rootID)
	if err != nil {
		return nil, err
	}

	// RLS: filter out entities the user cannot access
	scope := security.GetDataScope(ctx)
	if scope != nil && !scope.IsAdmin {
		filtered := make([]T, 0, len(items))
		for _, ent := range items {
			if security.CheckRLSAccess(ctx, s.entityName, ent) == nil {
				filtered = append(filtered, ent)
			}
		}
		items = filtered
	}

	// CEL: filter out entities denied by read rules
	items, _ = security.FilterByReadPolicy(ctx, s.policyEngine, s.entityName, items)

	// ERP Fix: Prune orphaned branches.
	// If a parent folder was filtered out by RLS/CEL, we must also remove its children,
	// otherwise the UI tree will break (children pointing to non-existent parents).
	if len(items) > 0 {
		allowedIDs := make(map[id.ID]bool, len(items))
		idToParent := make(map[id.ID]*id.ID, len(items))
		for _, ent := range items {
			eID := ent.GetID()
			allowedIDs[eID] = true
			if pAcc, ok := any(ent).(ParentAccessor); ok {
				idToParent[eID] = pAcc.GetParentID()
			}
		}

		pruned := make([]T, 0, len(items))
		for _, ent := range items {
			eID := ent.GetID()
			keep := true
			currPID := idToParent[eID]
			visited := map[id.ID]bool{eID: true}
			for currPID != nil {
				if visited[*currPID] {
					return nil, apperror.NewValidation("Hierarchy contains a cycle")
				}
				visited[*currPID] = true
				if rootID != nil && *currPID == *rootID {
					break
				}
				if !allowedIDs[*currPID] {
					keep = false
					break
				}
				currPID = idToParent[*currPID]
			}
			if keep {
				pruned = append(pruned, ent)
			}
		}
		items = pruned
	}

	return items, nil
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

// maxTakenCodes bounds how many codes of a series taken by hand are skipped
// before a write gives up.
const maxTakenCodes = 100

// assignCode applies the Definition's code rule as the last step before the
// item is written, inside the write's transaction: the counter moves only
// when the write commits. A code once given stays: an update without one
// keeps stored. An empty code of a series catalog gets the next code that no
// item holds; other sources require a code by now.
func (s *CatalogService[T]) assignCode(ctx context.Context, ent T, stored string) error {
	coded, ok := any(ent).(entity.Coded)
	if !ok {
		return nil
	}
	code := strings.TrimSpace(coded.GetCode())
	if code == "" {
		code = stored
	}
	coded.SetCode(code)
	if code != "" {
		return nil
	}
	if s.codeSource != entity.CodeFromSeries {
		return apperror.NewValidation("Укажите код").WithDetail("field", "code")
	}
	if s.numerator == nil {
		return apperror.NewInternal(fmt.Errorf("catalog %s numbers its codes but has no numerator", s.entityName))
	}
	for range maxTakenCodes {
		next, err := s.numerator.GetNextNumber(ctx, s.codeSeries, time.Time{})
		if err != nil {
			return fmt.Errorf("assign %s code: %w", s.entityName, err)
		}
		taken, err := s.repo.ExistsByCode(ctx, next)
		if err != nil {
			return fmt.Errorf("assign %s code: %w", s.entityName, err)
		}
		if !taken {
			coded.SetCode(next)
			return nil
		}
	}
	return apperror.NewInternal(fmt.Errorf("series %s: %d consecutive codes are already taken", s.codeSeries.Key, maxTakenCodes))
}

// storedCode is the code of the item as it is stored, empty for a catalog
// without codes.
func storedCode(item any) string {
	if coded, ok := item.(entity.Coded); ok {
		return strings.TrimSpace(coded.GetCode())
	}
	return ""
}

func normalizeCatalog(ent any) {
	if normalizable, ok := ent.(interface{ Normalize() }); ok {
		normalizable.Normalize()
	}
}
