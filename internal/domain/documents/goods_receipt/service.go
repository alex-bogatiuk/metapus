// Package goods_receipt provides the GoodsReceipt document service.
package goods_receipt

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
	"metapus/pkg/logger"
)

// Service provides business operations for goods receipt documents.
// In Database-per-Tenant architecture, TxManager is obtained from context.
type Service struct {
	repo          Repository
	postingEngine *posting.Engine
	numerator     numerator.Generator
	txManager     tx.Manager // Optional. If nil, obtained from context (DB-per-tenant).
	hooks         *domain.HookRegistry[*GoodsReceipt]
}

// NewService creates a new goods receipt service.
// In Database-per-Tenant, TxManager is obtained from context.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	numerator numerator.Generator,
	txManager tx.Manager,
) *Service {
	return &Service{
		repo:          repo,
		postingEngine: postingEngine,
		numerator:     numerator,
		txManager:     txManager,
		hooks:         domain.NewHookRegistry[*GoodsReceipt](),
	}
}

// Hooks returns the hook registry for registering callbacks.
func (s *Service) Hooks() *domain.HookRegistry[*GoodsReceipt] {
	return s.hooks
}

func (s *Service) getTxManager(ctx context.Context) (tx.Manager, error) {
	if s.txManager != nil {
		return s.txManager, nil
	}
	return tenant.GetTxManager(ctx)
}

// Create creates a new goods receipt document.
func (s *Service) Create(ctx context.Context, doc *GoodsReceipt) error {
	// Run before-create hooks (for enrichment, validation, etc.)
	if err := s.hooks.RunBeforeCreate(ctx, doc); err != nil {
		return err
	}

	// Validate
	if err := doc.Validate(ctx); err != nil {
		return err
	}

	// Generate number if empty
	if doc.Number == "" {
		cfg := numerator.DefaultConfig("GR")
		number, err := s.numerator.GetNextNumber(ctx, cfg, &numerator.Options{Strategy: NumeratorStrategy}, time.Now())
		if err != nil {
			return fmt.Errorf("generate number: %w", err)
		}
		doc.Number = number
	}

	// Create in transaction
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, doc); err != nil {
			return fmt.Errorf("create document: %w", err)
		}

		if err := s.repo.SaveLines(ctx, doc.ID, doc.Lines); err != nil {
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

	logger.Info(ctx, "goods receipt created",
		"id", doc.ID,
		"number", doc.Number)

	return nil
}

// GetByID retrieves a goods receipt with lines.
func (s *Service) GetByID(ctx context.Context, docID id.ID) (*GoodsReceipt, error) {
	doc, err := s.repo.GetByID(ctx, docID)
	if err != nil {
		return nil, err
	}

	lines, err := s.repo.GetLines(ctx, docID)
	if err != nil {
		return nil, fmt.Errorf("get lines: %w", err)
	}
	doc.Lines = lines

	return doc, nil
}

// Update updates a goods receipt document.
func (s *Service) Update(ctx context.Context, doc *GoodsReceipt) error {
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
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Update(ctx, doc); err != nil {
			return fmt.Errorf("update document: %w", err)
		}

		if err := s.repo.SaveLines(ctx, doc.ID, doc.Lines); err != nil {
			return fmt.Errorf("save lines: %w", err)
		}

		return nil
	})

	return err
}

// Delete soft-deletes a goods receipt.
func (s *Service) Delete(ctx context.Context, docID id.ID) error {
	doc, err := s.repo.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	// Cannot delete posted document
	if doc.Posted {
		return doc.CanModify()
	}

	return s.repo.Delete(ctx, docID)
}

// Post records document movements to registers.
func (s *Service) Post(ctx context.Context, docID id.ID) error {
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	updateDoc := func(ctx context.Context) error {
		return s.repo.Update(ctx, doc)
	}

	return s.postingEngine.Post(ctx, doc, updateDoc)
}

// Unpost reverses document movements.
func (s *Service) Unpost(ctx context.Context, docID id.ID) error {
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	updateDoc := func(ctx context.Context) error {
		return s.repo.Update(ctx, doc)
	}

	return s.postingEngine.Unpost(ctx, doc, updateDoc)
}

// PostAndSave posts document and saves changes atomically.
// Used when creating and posting in one operation.
func (s *Service) PostAndSave(ctx context.Context, doc *GoodsReceipt) error {
	// Validate
	if err := doc.Validate(ctx); err != nil {
		return err
	}

	// Generate number if empty
	if doc.Number == "" {
		cfg := numerator.DefaultConfig("GR")
		number, err := s.numerator.GetNextNumber(ctx, cfg, &numerator.Options{Strategy: NumeratorStrategy}, time.Now())
		if err != nil {
			return fmt.Errorf("generate number: %w", err)
		}
		doc.Number = number
	}

	updateDoc := func(ctx context.Context) error {
		if doc.Version == 1 {
			// New document - create
			if err := s.repo.Create(ctx, doc); err != nil {
				return err
			}
			return s.repo.SaveLines(ctx, doc.ID, doc.Lines)
		}
		// Existing document - update
		return s.repo.Update(ctx, doc)
	}

	return s.postingEngine.Post(ctx, doc, updateDoc)
}

// List retrieves goods receipts with filtering.
func (s *Service) List(ctx context.Context, filter ListFilter) (domain.ListResult[*GoodsReceipt], error) {
	return s.repo.List(ctx, filter)
}
