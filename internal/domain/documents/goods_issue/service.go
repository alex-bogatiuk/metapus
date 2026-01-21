package goods_issue

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/tx"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
	"metapus/pkg/logger"
	"metapus/pkg/numerator"
)

// Service provides business operations for goods issue documents.
// In Database-per-Tenant architecture, TxManager is obtained from context.
type Service struct {
	repo          Repository
	postingEngine *posting.Engine
	numerator     *numerator.Service
	txManager     tx.Manager // Optional. If nil, obtained from context (DB-per-tenant).
}

// NewService creates a new goods issue service.
// In Database-per-Tenant, TxManager is obtained from context.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	numerator *numerator.Service,
	txManager tx.Manager,
) *Service {
	return &Service{
		repo:          repo,
		postingEngine: postingEngine,
		numerator:     numerator,
		txManager:     txManager,
	}
}

func (s *Service) getTxManager(ctx context.Context) (tx.Manager, error) {
	if s.txManager != nil {
		return s.txManager, nil
	}
	return tenant.GetTxManager(ctx)
}

// Create creates a new goods issue document.
func (s *Service) Create(ctx context.Context, doc *GoodsIssue) error {
	if err := doc.Validate(ctx); err != nil {
		return err
	}

	if doc.Number == "" {
		cfg := numerator.DefaultConfig("GI")
		number, err := s.numerator.GetNextNumber(ctx, cfg, &numerator.Options{Strategy: NumeratorStrategy}, time.Now())
		if err != nil {
			return fmt.Errorf("generate number: %w", err)
		}
		doc.Number = number
	}

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

	logger.Info(ctx, "goods issue created", "id", doc.ID, "number", doc.Number)
	return nil
}

// GetByID retrieves a goods issue with lines.
func (s *Service) GetByID(ctx context.Context, docID id.ID) (*GoodsIssue, error) {
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

// Update updates a goods issue document.
func (s *Service) Update(ctx context.Context, doc *GoodsIssue) error {
	if err := doc.CanModify(); err != nil {
		return err
	}

	if err := doc.Validate(ctx); err != nil {
		return err
	}

	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Update(ctx, doc); err != nil {
			return fmt.Errorf("update document: %w", err)
		}
		if err := s.repo.SaveLines(ctx, doc.ID, doc.Lines); err != nil {
			return fmt.Errorf("save lines: %w", err)
		}
		return nil
	})
}

// Delete soft-deletes a goods issue.
func (s *Service) Delete(ctx context.Context, docID id.ID) error {
	doc, err := s.repo.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	if doc.Posted {
		return doc.CanModify()
	}

	return s.repo.Delete(ctx, docID)
}

// Post records document movements to registers.
// Will fail if insufficient stock (negative balance prevention).
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
func (s *Service) PostAndSave(ctx context.Context, doc *GoodsIssue) error {
	if err := doc.Validate(ctx); err != nil {
		return err
	}

	if doc.Number == "" {
		cfg := numerator.DefaultConfig("GI")
		number, err := s.numerator.GetNextNumber(ctx, cfg, &numerator.Options{Strategy: NumeratorStrategy}, time.Now())
		if err != nil {
			return fmt.Errorf("generate number: %w", err)
		}
		doc.Number = number
	}

	updateDoc := func(ctx context.Context) error {
		if doc.Version == 1 {
			if err := s.repo.Create(ctx, doc); err != nil {
				return err
			}
			return s.repo.SaveLines(ctx, doc.ID, doc.Lines)
		}
		return s.repo.Update(ctx, doc)
	}

	return s.postingEngine.Post(ctx, doc, updateDoc)
}

// List retrieves goods issues with filtering.
func (s *Service) List(ctx context.Context, filter ListFilter) (domain.ListResult[*GoodsIssue], error) {
	return s.repo.List(ctx, filter)
}
