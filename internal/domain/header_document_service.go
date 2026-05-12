package domain

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/core/tenant"
	"metapus/internal/core/tx"
	"metapus/internal/domain/posting"
	"metapus/pkg/logger"
)

// BaseHeaderDocumentServiceConfig configures the BaseHeaderDocumentService.
type BaseHeaderDocumentServiceConfig[T HeaderDocumentEntity] struct {
	Repo              HeaderDocumentRepository[T]
	PostingEngine     *posting.Engine
	Numerator         numerator.Generator
	TxManager         tx.Manager // Optional for Database-per-Tenant
	CurrencyResolver  CurrencyResolveStrategy
	PolicyEngine      *security.PolicyEngine // Optional — CEL policy evaluation
	NumeratorPrefix   string
	NumeratorStrategy numerator.Strategy
	EntityName        string // for logging (e.g., "crypto_invoice")
}

// BaseHeaderDocumentService provides generic business logic for header-only documents.
// Identical to BaseDocumentService but without line operations (GetLines/SaveLines).
// Used for documents where all business data lives in the header (e.g., CryptoInvoice,
// CryptoPayment, CryptoWithdrawal).
//
// Implements DocumentService[T] — handlers and decorators are agnostic to this distinction.
type BaseHeaderDocumentService[T HeaderDocumentEntity] struct {
	Repo              HeaderDocumentRepository[T]
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

// NewBaseHeaderDocumentService creates a new BaseHeaderDocumentService.
func NewBaseHeaderDocumentService[T HeaderDocumentEntity](cfg BaseHeaderDocumentServiceConfig[T]) *BaseHeaderDocumentService[T] {
	return &BaseHeaderDocumentService[T]{
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
func (s *BaseHeaderDocumentService[T]) GetHooks() *HookRegistry[T] {
	return s.hooks
}

// SetPolicyEngine sets the CEL policy engine after construction.
func (s *BaseHeaderDocumentService[T]) SetPolicyEngine(engine *security.PolicyEngine) {
	s.PolicyEngine = engine
}

// GetTxManager returns TxManager from config or context.
func (s *BaseHeaderDocumentService[T]) GetTxManager(ctx context.Context) (tx.Manager, error) {
	if s.TxManager != nil {
		return s.TxManager, nil
	}
	return tenant.GetTxManager(ctx)
}

// ResolveCurrency resolves the document currency using the resolution chain.
// No-op when CurrencyResolver is nil (e.g., crypto documents use tokens, not currencies).
func (s *BaseHeaderDocumentService[T]) ResolveCurrency(ctx context.Context, doc T) error {
	if s.CurrencyResolver == nil {
		return nil
	}
	var orgID id.ID
	if orgOwned, ok := any(doc).(OrganizationOwned); ok {
		orgID = orgOwned.GetOrganizationID()
	}
	currencyID, err := s.CurrencyResolver.ResolveForDocument(
		ctx,
		doc.GetCurrencyID(),
		doc.GetContractID(),
		orgID,
	)
	if err != nil {
		return err
	}
	doc.SetCurrencyID(currencyID)
	return nil
}

// GenerateNumber generates a document number if it is empty.
func (s *BaseHeaderDocumentService[T]) GenerateNumber(ctx context.Context, doc T) error {
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

// checkRLSAccess delegates to security.CheckRLSAccess.
func (s *BaseHeaderDocumentService[T]) checkRLSAccess(ctx context.Context, doc T) error {
	return security.CheckRLSAccess(ctx, s.EntityName, doc)
}

// checkCELPolicy delegates to security.CheckCELPolicy.
func (s *BaseHeaderDocumentService[T]) checkCELPolicy(ctx context.Context, action string, doc T) error {
	return security.CheckCELPolicy(ctx, s.PolicyEngine, s.EntityName, action, doc)
}

// Create creates a new document in a transaction (no lines).
func (s *BaseHeaderDocumentService[T]) Create(ctx context.Context, doc T) error {
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	if err := s.hooks.RunBeforeCreate(ctx, doc); err != nil {
		return err
	}
	if err := s.ResolveCurrency(ctx, doc); err != nil {
		return err
	}
	if err := doc.Validate(ctx); err != nil {
		return err
	}
	if err := s.checkCELPolicy(ctx, "create", doc); err != nil {
		return err
	}
	if err := s.GenerateNumber(ctx, doc); err != nil {
		return err
	}

	txm, err := s.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		return s.Repo.Create(ctx, doc)
	})
	if err != nil {
		return err
	}

	if err := s.hooks.RunAfterCreate(ctx, doc); err != nil {
		logger.Warn(ctx, "after-create hook failed", "error", err)
	}

	logger.Info(ctx, s.EntityName+" created",
		"id", doc.GetID(),
		"number", doc.GetNumber())

	return nil
}

// GetByID retrieves a document by ID (no lines to load).
func (s *BaseHeaderDocumentService[T]) GetByID(ctx context.Context, docID id.ID) (T, error) {
	doc, err := s.Repo.GetByID(ctx, docID)
	if err != nil {
		return doc, err
	}
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		var zero T
		return zero, err
	}
	if err := s.checkCELPolicy(ctx, "read", doc); err != nil {
		var zero T
		return zero, err
	}
	return doc, nil
}

// Update updates a document (must be unposted, no lines).
func (s *BaseHeaderDocumentService[T]) Update(ctx context.Context, doc T) error {
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		return err
	}

	oldDoc, err := s.Repo.GetByID(ctx, doc.GetID())
	if err != nil {
		return fmt.Errorf("fetch existing document: %w", err)
	}
	if err := s.checkRLSAccess(ctx, oldDoc); err != nil {
		return err
	}
	if writePolicy := security.GetFieldPolicy(ctx, s.EntityName, "write"); writePolicy != nil {
		if err := security.ValidateWrite(oldDoc, doc, writePolicy); err != nil {
			return err
		}
	}
	if err := s.checkCELPolicy(ctx, "update", oldDoc); err != nil {
		return err
	}

	if oldDoc.IsPosted() {
		doc.MarkPosted()
	} else {
		doc.MarkUnposted()
	}

	if err := s.hooks.RunBeforeUpdate(ctx, doc); err != nil {
		return err
	}
	if err := doc.CanModify(); err != nil {
		return err
	}
	if err := doc.Validate(ctx); err != nil {
		return err
	}

	txm, err := s.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		return s.Repo.Update(ctx, doc)
	})
}

// Delete soft-deletes a document (must be unposted).
func (s *BaseHeaderDocumentService[T]) Delete(ctx context.Context, docID id.ID) error {
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	doc, err := s.Repo.GetByID(ctx, docID)
	if err != nil {
		return err
	}
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		return err
	}
	if err := s.checkCELPolicy(ctx, "delete", doc); err != nil {
		return err
	}
	if err := doc.State().CanDelete(); err != nil {
		return err
	}
	return s.Repo.Delete(ctx, docID)
}

// SetDeletionMark sets or clears the deletion mark on a document.
func (s *BaseHeaderDocumentService[T]) SetDeletionMark(ctx context.Context, docID id.ID, marked bool) error {
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		return err
	}
	if err := s.checkCELPolicy(ctx, "delete", doc); err != nil {
		return err
	}

	if doc.IsDeletionMarked() == marked {
		return nil
	}

	if marked {
		if doc.IsPosted() {
			if err := s.checkCELPolicy(ctx, "unpost", doc); err != nil {
				return err
			}
			updateDocAndMark := func(ctx context.Context) error {
				doc.MarkDeleted()
				return s.Repo.Update(ctx, doc)
			}
			return s.PostingEngine.Unpost(ctx, doc, updateDocAndMark)
		}

		txm, err := s.GetTxManager(ctx)
		if err != nil {
			return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
		}
		return txm.RunInTransaction(ctx, func(ctx context.Context) error {
			doc.MarkDeleted()
			return s.Repo.Update(ctx, doc)
		})
	}

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
func (s *BaseHeaderDocumentService[T]) Post(ctx context.Context, docID id.ID) error {
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		return err
	}
	if err := s.checkCELPolicy(ctx, "post", doc); err != nil {
		return err
	}

	updateDoc := func(ctx context.Context) error {
		return s.Repo.Update(ctx, doc)
	}
	return s.PostingEngine.Post(ctx, doc, updateDoc)
}

// Unpost reverses document movements.
func (s *BaseHeaderDocumentService[T]) Unpost(ctx context.Context, docID id.ID) error {
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		return err
	}
	if err := s.checkCELPolicy(ctx, "unpost", doc); err != nil {
		return err
	}

	updateDoc := func(ctx context.Context) error {
		return s.Repo.Update(ctx, doc)
	}
	return s.PostingEngine.Unpost(ctx, doc, updateDoc)
}

// PostAndSave posts document and saves changes atomically (no lines).
func (s *BaseHeaderDocumentService[T]) PostAndSave(ctx context.Context, doc T) error {
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	if err := s.hooks.RunBeforeCreate(ctx, doc); err != nil {
		return err
	}
	if err := s.ResolveCurrency(ctx, doc); err != nil {
		return err
	}
	if err := s.checkCELPolicy(ctx, "create", doc); err != nil {
		return err
	}
	if err := s.GenerateNumber(ctx, doc); err != nil {
		return err
	}

	updateDoc := func(ctx context.Context) error {
		if doc.GetVersion() == 1 {
			return s.Repo.Create(ctx, doc)
		}
		return s.Repo.Update(ctx, doc)
	}
	return s.PostingEngine.Post(ctx, doc, updateDoc)
}

// UpdateAndRepost atomically updates a posted document and re-posts it (no lines).
func (s *BaseHeaderDocumentService[T]) UpdateAndRepost(ctx context.Context, doc T) error {
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return err
	}
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		return err
	}

	oldDoc, err := s.Repo.GetByID(ctx, doc.GetID())
	if err != nil {
		return fmt.Errorf("fetch existing document: %w", err)
	}
	if err := s.checkRLSAccess(ctx, oldDoc); err != nil {
		return err
	}
	if err := s.checkCELPolicy(ctx, "update", oldDoc); err != nil {
		return err
	}
	if writePolicy := security.GetFieldPolicy(ctx, s.EntityName, "write"); writePolicy != nil {
		if err := security.ValidateWrite(oldDoc, doc, writePolicy); err != nil {
			return err
		}
	}

	if oldDoc.IsPosted() {
		doc.MarkPosted()
	} else {
		doc.MarkUnposted()
	}

	if err := s.hooks.RunBeforeUpdate(ctx, doc); err != nil {
		return err
	}
	if err := s.ResolveCurrency(ctx, doc); err != nil {
		return err
	}

	updateDoc := func(ctx context.Context) error {
		return s.Repo.Update(ctx, doc)
	}
	return s.PostingEngine.Post(ctx, doc, updateDoc)
}

// List retrieves documents with cursor-based pagination.
func (s *BaseHeaderDocumentService[T]) List(ctx context.Context, filter ListFilter) (CursorListResult[T], error) {
	if filter.DataScope == nil {
		filter.DataScope = security.GetDataScope(ctx)
	}
	result, err := s.Repo.List(ctx, filter)
	if err != nil {
		return result, err
	}

	filtered, removed := security.FilterByReadPolicy(ctx, s.PolicyEngine, s.EntityName, result.Items)
	result.Items = filtered
	if result.TotalCount != nil && removed > 0 {
		adjusted := *result.TotalCount - int64(removed)
		result.TotalCount = &adjusted
	}
	return result, nil
}

// ListIDs returns all document IDs matching the filter.
func (s *BaseHeaderDocumentService[T]) ListIDs(ctx context.Context, filter ListFilter, maxIDs int) ([]id.ID, error) {
	if filter.DataScope == nil {
		filter.DataScope = security.GetDataScope(ctx)
	}
	return s.Repo.ListIDs(ctx, filter, maxIDs)
}
