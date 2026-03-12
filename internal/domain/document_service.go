package domain

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/core/tenant"
	"metapus/internal/core/tx"
	"metapus/internal/domain/posting"
	"metapus/pkg/logger"
)

// DocumentEntity is the combined constraint for document types used by BaseDocumentService.
// T must be a pointer to a struct embedding entity.Document + entity.CurrencyAware
// and implementing posting.Postable.
type DocumentEntity[L any] interface {
	entity.Validatable
	posting.Postable
	LinesAccessor[L]
	CurrencyAwareDoc
	State() entity.DocumentState
	CanModify() error
	IsDeletionMarked() bool
	MarkDeleted()
	Undelete()
	GetNumber() string
	SetNumber(string)
	GetVersion() int
}

// BaseDocumentServiceConfig configures the BaseDocumentService.
type BaseDocumentServiceConfig[T DocumentEntity[L], L any] struct {
	Repo              DocumentRepository[T, L]
	PostingEngine     *posting.Engine
	Numerator         numerator.Generator
	TxManager         tx.Manager // Optional for Database-per-Tenant
	CurrencyResolver  CurrencyResolveStrategy
	PolicyEngine      *security.PolicyEngine // Optional — CEL policy evaluation
	NumeratorPrefix   string
	NumeratorStrategy numerator.Strategy
	EntityName        string // for logging (e.g., "goods receipt", "goods issue")
}

// BaseDocumentService provides generic business logic for document entities.
// Implements the Template Method pattern: common operations are defined here,
// document-specific behaviour is provided via interfaces on T.
type BaseDocumentService[T DocumentEntity[L], L any] struct {
	Repo              DocumentRepository[T, L]
	PostingEngine     *posting.Engine
	Numerator         numerator.Generator
	TxManager         tx.Manager
	CurrencyResolver  CurrencyResolveStrategy
	PolicyEngine      *security.PolicyEngine
	hooks             *HookRegistry[T]
	NumeratorPrefix   string
	NumeratorStrategy numerator.Strategy
	EntityName        string
}

// NewBaseDocumentService creates a new BaseDocumentService.
func NewBaseDocumentService[T DocumentEntity[L], L any](cfg BaseDocumentServiceConfig[T, L]) *BaseDocumentService[T, L] {
	return &BaseDocumentService[T, L]{
		Repo:              cfg.Repo,
		PostingEngine:     cfg.PostingEngine,
		Numerator:         cfg.Numerator,
		TxManager:         cfg.TxManager,
		CurrencyResolver:  cfg.CurrencyResolver,
		PolicyEngine:      cfg.PolicyEngine,
		hooks:             NewHookRegistry[T](),
		NumeratorPrefix:   cfg.NumeratorPrefix,
		NumeratorStrategy: cfg.NumeratorStrategy,
		EntityName:        cfg.EntityName,
	}
}

// GetHooks returns the hook registry for external registration.
func (s *BaseDocumentService[T, L]) GetHooks() *HookRegistry[T] {
	return s.hooks
}

// SetPolicyEngine sets the CEL policy engine after construction.
func (s *BaseDocumentService[T, L]) SetPolicyEngine(engine *security.PolicyEngine) {
	s.PolicyEngine = engine
}

// GetTxManager returns TxManager from config or context.
func (s *BaseDocumentService[T, L]) GetTxManager(ctx context.Context) (tx.Manager, error) {
	if s.TxManager != nil {
		return s.TxManager, nil
	}
	return tenant.GetTxManager(ctx)
}

// ResolveCurrency resolves the document currency using the resolution chain.
func (s *BaseDocumentService[T, L]) ResolveCurrency(ctx context.Context, doc T) error {
	currencyID, err := s.CurrencyResolver.ResolveForDocument(
		ctx,
		doc.GetCurrencyID(),
		doc.GetContractID(),
		doc.GetOrganizationID(),
	)
	if err != nil {
		return err
	}
	doc.SetCurrencyID(currencyID)
	return nil
}

// GenerateNumber generates a document number if it is empty.
func (s *BaseDocumentService[T, L]) GenerateNumber(ctx context.Context, doc T) error {
	if doc.GetNumber() != "" {
		return nil
	}
	cfg := numerator.DefaultConfig(s.NumeratorPrefix)
	number, err := s.Numerator.GetNextNumber(ctx, cfg, &numerator.Options{Strategy: s.NumeratorStrategy}, time.Now())
	if err != nil {
		return fmt.Errorf("generate number: %w", err)
	}
	doc.SetNumber(number)
	return nil
}

// checkRLSAccess verifies that the current user's DataScope allows access to the document.
// Uses the RLSDimensionable interface if the document implements it.
func (s *BaseDocumentService[T, L]) checkRLSAccess(ctx context.Context, doc T) error {
	scope := security.GetDataScope(ctx)
	if scope == nil || scope.IsAdmin {
		return nil
	}
	if dimensionable, ok := any(doc).(security.RLSDimensionable); ok {
		if !scope.CanAccessRecord(dimensionable.GetRLSDimensions()) {
			return apperror.NewForbidden("access denied by row-level security")
		}
	}
	return nil
}

// checkCELPolicy evaluates CEL policy rules for the given action and document.
// Returns nil if no PolicyEngine is configured or no rules match.
func (s *BaseDocumentService[T, L]) checkCELPolicy(ctx context.Context, action string, doc T) error {
	if s.PolicyEngine == nil {
		return nil
	}
	rules := security.GetApplicableRules(ctx, s.EntityName, action)
	if len(rules) == 0 {
		return nil
	}
	return s.PolicyEngine.Evaluate(ctx, rules, action, doc)
}

// Create creates a new document with lines in a transaction.
func (s *BaseDocumentService[T, L]) Create(ctx context.Context, doc T) error {
	// RLS: check write permission
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}

	// Run before-create hooks (for enrichment, validation, etc.)
	if err := s.hooks.RunBeforeCreate(ctx, doc); err != nil {
		return err
	}

	// Resolve currency
	if err := s.ResolveCurrency(ctx, doc); err != nil {
		return err
	}

	// Validate
	if err := doc.Validate(ctx); err != nil {
		return err
	}

	// CEL policy check
	if err := s.checkCELPolicy(ctx, "create", doc); err != nil {
		return err
	}

	// Generate number if empty
	if err := s.GenerateNumber(ctx, doc); err != nil {
		return err
	}

	// Create in transaction
	txm, err := s.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.Repo.Create(ctx, doc); err != nil {
			return fmt.Errorf("create document: %w", err)
		}
		if err := s.Repo.SaveLines(ctx, doc.GetID(), doc.GetLines()); err != nil {
			return fmt.Errorf("save lines: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Run after-create hooks
	if err := s.hooks.RunAfterCreate(ctx, doc); err != nil {
		logger.Warn(ctx, "after-create hook failed", "error", err)
	}

	logger.Info(ctx, s.EntityName+" created",
		"id", doc.GetID(),
		"number", doc.GetNumber())

	return nil
}

// GetByID retrieves a document with its lines.
// Performs RLS point-check after loading the document.
func (s *BaseDocumentService[T, L]) GetByID(ctx context.Context, docID id.ID) (T, error) {
	doc, err := s.Repo.GetByID(ctx, docID)
	if err != nil {
		return doc, err
	}

	// RLS point-check: verify user can access this specific record
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		var zero T
		return zero, err
	}

	// CEL policy check for read
	if err := s.checkCELPolicy(ctx, "read", doc); err != nil {
		var zero T
		return zero, err
	}

	lines, err := s.Repo.GetLines(ctx, docID)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("get lines: %w", err)
	}
	doc.SetLines(lines)

	return doc, nil
}

// Update updates a document (must be unposted).
func (s *BaseDocumentService[T, L]) Update(ctx context.Context, doc T) error {
	// RLS: check write permission and record access
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		return err
	}

	// Fetch existing document for FLS validation and CEL evaluation.
	// CEL rules must evaluate against the existing state (e.g. doc.posted == true),
	// not the input entity where fields default to zero values.
	oldDoc, err := s.Repo.GetByID(ctx, doc.GetID())
	if err != nil {
		return fmt.Errorf("fetch existing document: %w", err)
	}

	// FLS: validate that no restricted fields were modified
	if writePolicy := security.GetFieldPolicy(ctx, s.EntityName, "write"); writePolicy != nil {
		if err := security.NewFieldMasker().ValidateWrite(oldDoc, doc, writePolicy); err != nil {
			return err
		}
	}

	// CEL policy check — evaluate against existing document state
	if err := s.checkCELPolicy(ctx, "update", oldDoc); err != nil {
		return err
	}

	// Run before-update hooks
	if err := s.hooks.RunBeforeUpdate(ctx, doc); err != nil {
		return err
	}

	// Check if can modify
	if err := doc.CanModify(); err != nil {
		return err
	}

	// Validate
	if err := doc.Validate(ctx); err != nil {
		return err
	}

	// Update in transaction
	txm, err := s.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.Repo.Update(ctx, doc); err != nil {
			return fmt.Errorf("update document: %w", err)
		}
		if err := s.Repo.SaveLines(ctx, doc.GetID(), doc.GetLines()); err != nil {
			return fmt.Errorf("save lines: %w", err)
		}
		return nil
	})
}

// Delete soft-deletes a document (must be unposted).
// Delegates permission check to the document's lifecycle state.
func (s *BaseDocumentService[T, L]) Delete(ctx context.Context, docID id.ID) error {
	// RLS: check write permission
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}

	doc, err := s.Repo.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	// RLS point-check
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		return err
	}

	// CEL policy check
	if err := s.checkCELPolicy(ctx, "delete", doc); err != nil {
		return err
	}

	// State pattern: delegate permission check to current lifecycle state
	if err := doc.State().CanDelete(); err != nil {
		return err
	}

	return s.Repo.Delete(ctx, docID)
}

// SetDeletionMark sets or clears the deletion mark on a document.
// 1C-style logic:
//   - If marking for deletion (marked=true) and document is posted: unpost first, then mark deleted (atomic).
//   - If marking for deletion (marked=true) and document is NOT posted: just mark deleted.
//   - If clearing the mark (marked=false): clear the flag, document stays unposted (draft state).
func (s *BaseDocumentService[T, L]) SetDeletionMark(ctx context.Context, docID id.ID, marked bool) error {
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	// No-op if state already matches
	if doc.IsDeletionMarked() == marked {
		return nil
	}

	if marked {
		// Setting deletion mark
		if doc.IsPosted() {
			// Unpost + mark deleted in one transaction via postingEngine.Unpost
			updateDocAndMark := func(ctx context.Context) error {
				// At this point MarkUnposted() has been called by the engine,
				// movements have been reversed. We just need to set deletion mark.
				doc.MarkDeleted()
				return s.Repo.Update(ctx, doc)
			}
			return s.PostingEngine.Unpost(ctx, doc, updateDocAndMark)
		}

		// Not posted — just mark in a transaction
		txm, err := s.GetTxManager(ctx)
		if err != nil {
			return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
		}
		return txm.RunInTransaction(ctx, func(ctx context.Context) error {
			doc.MarkDeleted()
			return s.Repo.Update(ctx, doc)
		})
	}

	// Clearing deletion mark (marked=false)
	// Document stays unposted — user must explicitly re-post if needed.
	txm, err := s.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		doc.Undelete()
		return s.Repo.Update(ctx, doc)
	})
}

// Post records document movements to registers.
func (s *BaseDocumentService[T, L]) Post(ctx context.Context, docID id.ID) error {
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	// CEL policy check
	if err := s.checkCELPolicy(ctx, "post", doc); err != nil {
		return err
	}

	updateDoc := func(ctx context.Context) error {
		return s.Repo.Update(ctx, doc)
	}

	return s.PostingEngine.Post(ctx, doc, updateDoc)
}

// Unpost reverses document movements.
func (s *BaseDocumentService[T, L]) Unpost(ctx context.Context, docID id.ID) error {
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	// CEL policy check
	if err := s.checkCELPolicy(ctx, "unpost", doc); err != nil {
		return err
	}

	updateDoc := func(ctx context.Context) error {
		return s.Repo.Update(ctx, doc)
	}

	return s.PostingEngine.Unpost(ctx, doc, updateDoc)
}

// PostAndSave posts document and saves changes atomically.
// Used when creating and posting in one operation.
func (s *BaseDocumentService[T, L]) PostAndSave(ctx context.Context, doc T) error {
	// Run before-create hooks (for enrichment: CreatedBy, UpdatedBy, etc.)
	if err := s.hooks.RunBeforeCreate(ctx, doc); err != nil {
		return err
	}

	// Resolve currency
	if err := s.ResolveCurrency(ctx, doc); err != nil {
		return err
	}

	// Generate number if empty
	if err := s.GenerateNumber(ctx, doc); err != nil {
		return err
	}

	updateDoc := func(ctx context.Context) error {
		if doc.GetVersion() == 1 {
			// New document - create
			if err := s.Repo.Create(ctx, doc); err != nil {
				return err
			}
			return s.Repo.SaveLines(ctx, doc.GetID(), doc.GetLines())
		}
		// Existing document - update
		return s.Repo.Update(ctx, doc)
	}

	return s.PostingEngine.Post(ctx, doc, updateDoc)
}

// UpdateAndRepost atomically updates a posted document and re-posts it.
// This is the 1C-style "Записать проведённый документ" — a single transaction:
//   - Engine detects doc.IsPosted() → reverses old movements
//   - Saves updated document + lines
//   - Generates new movements
//   - Validates stock availability
//   - Records new movements
//
// If the document is not posted, behaves like Update + Post.
func (s *BaseDocumentService[T, L]) UpdateAndRepost(ctx context.Context, doc T) error {
	// RLS: check write permission and record access
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		return err
	}

	// Fetch existing document for CEL evaluation against current state.
	oldDoc, err := s.Repo.GetByID(ctx, doc.GetID())
	if err != nil {
		return fmt.Errorf("fetch existing document: %w", err)
	}

	// CEL policy check — evaluate against existing document state
	if err := s.checkCELPolicy(ctx, "update", oldDoc); err != nil {
		return err
	}

	// Run before-update hooks
	if err := s.hooks.RunBeforeUpdate(ctx, doc); err != nil {
		return err
	}

	// Resolve currency
	if err := s.ResolveCurrency(ctx, doc); err != nil {
		return err
	}

	// Validate
	if err := doc.Validate(ctx); err != nil {
		return err
	}

	updateDoc := func(ctx context.Context) error {
		if err := s.Repo.Update(ctx, doc); err != nil {
			return fmt.Errorf("update document: %w", err)
		}
		return s.Repo.SaveLines(ctx, doc.GetID(), doc.GetLines())
	}

	return s.PostingEngine.Post(ctx, doc, updateDoc)
}

// List retrieves documents with cursor-based pagination.
// Defense-in-depth: if DataScope was not injected by handler (ParseListFilter),
// fall back to context — ensures RLS is enforced even for direct service calls.
func (s *BaseDocumentService[T, L]) List(ctx context.Context, filter ListFilter) (CursorListResult[T], error) {
	if filter.DataScope == nil {
		filter.DataScope = security.GetDataScope(ctx)
	}
	return s.Repo.List(ctx, filter)
}
