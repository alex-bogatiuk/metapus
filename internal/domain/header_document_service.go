package domain

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/core/tenant"
	"metapus/internal/core/tx"
	"metapus/internal/domain/posting"
)

// BaseHeaderDocumentServiceConfig configures the BaseHeaderDocumentService.
type BaseHeaderDocumentServiceConfig[T HeaderDocumentEntity] struct {
	Definition       *entity.Definition
	Repo             HeaderDocumentRepository[T]
	PostingEngine    *posting.Engine
	Numerator        numerator.Generator
	TxManager        tx.Manager // Optional for Database-per-Tenant
	CurrencyResolver CurrencyResolveStrategy
	PolicyEngine     *security.PolicyEngine // Optional — CEL policy evaluation
	NumeratorPrefix  string
	EntityName       string // for logging (e.g., "crypto_invoice")
	// Parts persists the document's table parts with it; nil for a
	// document whose data lives in the header alone.
	Parts DocumentParts[T]
}

// DocumentParts loads and stores the table parts of a document together with
// its header. SaveParts replaces the stored rows with the document's own and
// runs inside the transaction that writes the header.
type DocumentParts[T any] interface {
	LoadParts(ctx context.Context, doc T) error
	SaveParts(ctx context.Context, doc T) error
}

// BaseHeaderDocumentService provides generic business logic for documents
// whose header is one row (e.g., CryptoInvoice, CryptoPayment). Unlike
// BaseDocumentService it is not generic over one line type: table parts, any
// number of them, are loaded and stored with the header through Parts.
//
// Implements DocumentService[T] — handlers and decorators are agnostic to this distinction.
type BaseHeaderDocumentService[T HeaderDocumentEntity] struct {
	Repo             HeaderDocumentRepository[T]
	PostingEngine    *posting.Engine
	Numerator        numerator.Generator
	TxManager        tx.Manager
	CurrencyResolver CurrencyResolveStrategy
	PolicyEngine     *security.PolicyEngine
	hooks            *HookRegistry[T]
	NumeratorPrefix  string
	EntityName       string
	parts            DocumentParts[T]
}

// NewBaseHeaderDocumentService creates a new BaseHeaderDocumentService.
func NewBaseHeaderDocumentService[T HeaderDocumentEntity](cfg BaseHeaderDocumentServiceConfig[T]) *BaseHeaderDocumentService[T] {
	if cfg.Definition != nil {
		if err := cfg.Definition.Validate(); err != nil {
			panic(err)
		}
		if cfg.Definition.Kind != entity.DocumentKind {
			panic("document service requires a document definition")
		}
		cfg.EntityName = cfg.Definition.Key
	}

	return &BaseHeaderDocumentService[T]{
		Repo:             cfg.Repo,
		PostingEngine:    cfg.PostingEngine,
		Numerator:        cfg.Numerator,
		TxManager:        cfg.TxManager,
		CurrencyResolver: cfg.CurrencyResolver,
		PolicyEngine:     cfg.PolicyEngine,
		hooks:            NewHookRegistry[T](),
		NumeratorPrefix:  cfg.NumeratorPrefix,
		EntityName:       cfg.EntityName,
		parts:            cfg.Parts,
	}
}

// loadParts fills the table parts of a document read from the repository.
func (s *BaseHeaderDocumentService[T]) loadParts(ctx context.Context, doc T) error {
	if s.parts == nil {
		return nil
	}
	return s.parts.LoadParts(ctx, doc)
}

// saveParts stores the table parts of a document whose header was written.
func (s *BaseHeaderDocumentService[T]) saveParts(ctx context.Context, doc T) error {
	if s.parts == nil {
		return nil
	}
	return s.parts.SaveParts(ctx, doc)
}

// getStored reads a document with its table parts, without access checks.
func (s *BaseHeaderDocumentService[T]) getStored(ctx context.Context, docID id.ID) (T, error) {
	doc, err := s.Repo.GetByID(ctx, docID)
	if err != nil {
		return doc, err
	}
	if err := s.loadParts(ctx, doc); err != nil {
		var zero T
		return zero, err
	}
	return doc, nil
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
// No-op without a resolver or when the document does not implement CurrencyAwareDoc.
func (s *BaseHeaderDocumentService[T]) ResolveCurrency(ctx context.Context, doc T) error {
	return resolveDocumentCurrency(ctx, doc, s.CurrencyResolver)
}

// GenerateNumber generates a document number if it is empty.
func (s *BaseHeaderDocumentService[T]) GenerateNumber(ctx context.Context, doc T) error {
	if doc.GetNumber() != "" {
		return nil
	}
	cfg := numerator.DefaultConfig(s.NumeratorPrefix)
	number, err := s.Numerator.GetNextNumber(ctx, cfg, numberPeriod(doc))
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

// Create creates a new document and its table parts in a transaction.
func (s *BaseHeaderDocumentService[T]) Create(ctx context.Context, doc T) error {
	return runDocumentMutation(ctx, s.GetTxManager, func(ctx context.Context) error {
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

		if err := s.Repo.Create(ctx, doc); err != nil {
			return err
		}
		if err := s.saveParts(ctx, doc); err != nil {
			return err
		}
		return completeDocumentMutation(ctx, s.hooks, doc, TransactionCreate, AfterCreate)
	})
}

// GetByID retrieves a document by ID with its table parts.
func (s *BaseHeaderDocumentService[T]) GetByID(ctx context.Context, docID id.ID) (T, error) {
	doc, err := s.Repo.GetByID(ctx, docID)
	if err != nil {
		return doc, err
	}
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		var zero T
		return zero, err
	}
	if err := s.loadParts(ctx, doc); err != nil {
		var zero T
		return zero, err
	}
	if err := s.checkCELPolicy(ctx, "read", doc); err != nil {
		var zero T
		return zero, err
	}
	return doc, nil
}

func (s *BaseHeaderDocumentService[T]) GetByIDForUpdate(ctx context.Context, docID id.ID) (T, error) {
	doc, err := s.Repo.GetByIDForUpdate(ctx, docID)
	if err != nil {
		return doc, err
	}
	if err := s.checkRLSAccess(ctx, doc); err != nil {
		var zero T
		return zero, err
	}
	if err := s.loadParts(ctx, doc); err != nil {
		var zero T
		return zero, err
	}
	if err := s.checkCELPolicy(ctx, "read", doc); err != nil {
		var zero T
		return zero, err
	}
	return doc, nil
}

// Update updates a document and replaces its table parts (must be unposted).
func (s *BaseHeaderDocumentService[T]) Update(ctx context.Context, doc T) error {
	return runDocumentMutation(ctx, s.GetTxManager, func(ctx context.Context) error {
		if err := s.checkRLSAccess(ctx, doc); err != nil {
			return err
		}

		oldDoc, err := s.getStored(ctx, doc.GetID())
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

		if err := s.Repo.Update(ctx, doc); err != nil {
			return err
		}
		if err := s.saveParts(ctx, doc); err != nil {
			return err
		}
		return completeDocumentMutation(ctx, s.hooks, doc, TransactionUpdate, AfterUpdate)
	})
}

// Delete soft-deletes a document (must be unposted).
func (s *BaseHeaderDocumentService[T]) Delete(ctx context.Context, docID id.ID) error {
	return runDocumentMutation(ctx, s.GetTxManager, func(ctx context.Context) error {
		if err := security.GetDataScope(ctx).CanMutate(); err != nil {
			return err
		}
		doc, err := s.getStored(ctx, docID)
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
		if err := s.hooks.RunBeforeDelete(ctx, doc); err != nil {
			return err
		}
		if err := s.Repo.Delete(ctx, docID); err != nil {
			return err
		}
		return completeDocumentMutation(ctx, s.hooks, doc, TransactionDelete, AfterDelete)
	})
}

// SetDeletionMark sets or clears the deletion mark on a document.
func (s *BaseHeaderDocumentService[T]) SetDeletionMark(ctx context.Context, docID id.ID, marked bool) error {
	return runDocumentMutation(ctx, s.GetTxManager, func(ctx context.Context) error {
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
		persist := func(ctx context.Context) error {
			phase := TransactionClearDeletionMark
			if marked {
				doc.MarkDeleted()
				phase = TransactionSetDeletionMark
			} else {
				doc.Undelete()
			}
			if err := s.Repo.Update(ctx, doc); err != nil {
				return err
			}
			return completeDocumentMutation(ctx, s.hooks, doc, phase, "")
		}
		if marked && doc.IsPosted() {
			if err := s.checkCELPolicy(ctx, "unpost", doc); err != nil {
				return err
			}
			return s.PostingEngine.Unpost(ctx, doc, persist)
		}
		return persist(ctx)
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

// PostAndSave posts document and saves changes atomically.
func (s *BaseHeaderDocumentService[T]) PostAndSave(ctx context.Context, doc T) error {
	return runDocumentMutation(ctx, s.GetTxManager, func(ctx context.Context) error {
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
				if err := s.Repo.Create(ctx, doc); err != nil {
					return err
				}
				if err := s.saveParts(ctx, doc); err != nil {
					return err
				}
				return completeDocumentMutation(ctx, s.hooks, doc, TransactionCreate, AfterCreate)
			}
			if err := s.Repo.Update(ctx, doc); err != nil {
				return err
			}
			if err := s.saveParts(ctx, doc); err != nil {
				return err
			}
			return completeDocumentMutation(ctx, s.hooks, doc, TransactionUpdate, AfterUpdate)
		}
		return s.PostingEngine.Post(ctx, doc, updateDoc)
	})
}

// UpdateAndRepost atomically updates a posted document and re-posts it.
func (s *BaseHeaderDocumentService[T]) UpdateAndRepost(ctx context.Context, doc T) error {
	return runDocumentMutation(ctx, s.GetTxManager, func(ctx context.Context) error {
		if err := security.GetDataScope(ctx).CanMutate(); err != nil {
			return err
		}
		if err := s.checkRLSAccess(ctx, doc); err != nil {
			return err
		}

		oldDoc, err := s.getStored(ctx, doc.GetID())
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
			if err := s.Repo.Update(ctx, doc); err != nil {
				return err
			}
			if err := s.saveParts(ctx, doc); err != nil {
				return err
			}
			return completeDocumentMutation(ctx, s.hooks, doc, TransactionUpdate, AfterUpdate)
		}
		return s.PostingEngine.Post(ctx, doc, updateDoc)
	})
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
