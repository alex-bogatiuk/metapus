package inventory

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/core/tx"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/stock"
	"metapus/pkg/logger"
)

// Service provides business operations for inventory documents.
// In Database-per-Tenant architecture, TxManager is obtained from context.
type Service struct {
	repo          Repository
	postingEngine *posting.Engine
	stockService  *stock.Service
	numerator     numerator.Generator
	txManager     tx.Manager // Optional. If nil, obtained from context (DB-per-tenant).
	hooks         *domain.HookRegistry[*Inventory]
}

// NewService creates a new inventory service.
// In Database-per-Tenant, TxManager is obtained from context.
func NewService(
	repo Repository,
	postingEngine *posting.Engine,
	stockService *stock.Service,
	numerator numerator.Generator,
	txManager tx.Manager,
) *Service {
	return &Service{
		repo:          repo,
		postingEngine: postingEngine,
		stockService:  stockService,
		numerator:     numerator,
		txManager:     txManager,
		hooks:         domain.NewHookRegistry[*Inventory](),
	}
}

// Hooks returns the hook registry for registering callbacks.
func (s *Service) Hooks() *domain.HookRegistry[*Inventory] {
	return s.hooks
}

func (s *Service) getTxManager(ctx context.Context) (tx.Manager, error) {
	if s.txManager != nil {
		return s.txManager, nil
	}
	return tenant.GetTxManager(ctx)
}

// Create creates a new inventory document.
func (s *Service) Create(ctx context.Context, doc *Inventory) error {
	// Run before-create hooks
	if err := s.hooks.RunBeforeCreate(ctx, doc); err != nil {
		return err
	}

	if err := doc.Validate(ctx); err != nil {
		return err
	}

	if doc.Number == "" {
		cfg := numerator.DefaultConfig("INV")
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

	// Run after-create hooks
	if err := s.hooks.RunAfterCreate(ctx, doc); err != nil {
		logger.Warn(ctx, "after-create hook failed", "error", err)
	}

	logger.Info(ctx, "inventory created", "id", doc.ID, "number", doc.Number)
	return nil
}

// GetByID retrieves an inventory with lines.
func (s *Service) GetByID(ctx context.Context, docID id.ID) (*Inventory, error) {
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

// Update updates an inventory document.
func (s *Service) Update(ctx context.Context, doc *Inventory) error {
	// Run before-update hooks
	if err := s.hooks.RunBeforeUpdate(ctx, doc); err != nil {
		return err
	}

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

// Delete soft-deletes an inventory.
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

// PrepareSheet prepares inventory sheet by loading current stock balances.
func (s *Service) PrepareSheet(ctx context.Context, docID id.ID) (*Inventory, error) {
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return nil, err
	}

	if doc.Status != StatusDraft {
		return nil, fmt.Errorf("can only prepare sheet in draft status")
	}

	// Get current stock for warehouse
	balances, err := s.stockService.GetWarehouseStock(ctx, doc.WarehouseID)
	if err != nil {
		return nil, fmt.Errorf("get warehouse stock: %w", err)
	}

	// Create lines from balances
	doc.Lines = make([]InventoryLine, 0, len(balances))
	for _, balance := range balances {
		doc.AddLine(balance.ProductID, balance.Quantity, 0) // unitPrice будет заполнен позже
	}

	// Save lines
	txm, err := s.getTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		return s.repo.SaveLines(ctx, doc.ID, doc.Lines)
	})
	if err != nil {
		return nil, err
	}

	return doc, nil
}

// Start starts the inventory process.
func (s *Service) Start(ctx context.Context, docID id.ID) error {
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	if err := doc.Start(); err != nil {
		return err
	}

	return s.repo.Update(ctx, doc)
}

// RecordCount records actual quantity for a line.
func (s *Service) RecordCount(ctx context.Context, docID id.ID, lineNo int, actualQty types.Quantity, countedBy string) error {
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	if doc.Status != StatusInProgress {
		return fmt.Errorf("can only record counts in in_progress status")
	}

	if err := doc.SetActualQuantity(lineNo, actualQty, countedBy); err != nil {
		return err
	}

	txm, err := s.getTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err).WithDetail("missing", "tx_manager")
	}
	return txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Update(ctx, doc); err != nil {
			return err
		}
		return s.repo.SaveLines(ctx, doc.ID, doc.Lines)
	})
}

// Complete completes the inventory.
func (s *Service) Complete(ctx context.Context, docID id.ID) error {
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	if err := doc.Complete(); err != nil {
		return err
	}

	return s.repo.Update(ctx, doc)
}

// Cancel cancels the inventory.
func (s *Service) Cancel(ctx context.Context, docID id.ID) error {
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return err
	}

	if err := doc.Cancel(); err != nil {
		return err
	}

	return s.repo.Update(ctx, doc)
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

// List retrieves inventories with filtering.
func (s *Service) List(ctx context.Context, filter ListFilter) (domain.ListResult[*Inventory], error) {
	return s.repo.List(ctx, filter)
}

// GetComparison returns comparison of book vs actual quantities.
func (s *Service) GetComparison(ctx context.Context, docID id.ID) (*ComparisonResult, error) {
	doc, err := s.GetByID(ctx, docID)
	if err != nil {
		return nil, err
	}

	result := &ComparisonResult{
		InventoryID: docID,
		WarehouseID: doc.WarehouseID,
		Status:      doc.Status,
		Items:       make([]ComparisonItem, 0, len(doc.Lines)),
	}

	for _, line := range doc.Lines {
		item := ComparisonItem{
			LineNo:       line.LineNo,
			ProductID:    line.ProductID,
			BookQuantity: line.BookQuantity,
			Counted:      line.Counted,
		}

		if line.ActualQuantity != nil {
			item.ActualQuantity = *line.ActualQuantity
			item.Deviation = line.Deviation
			item.DeviationAmount = line.DeviationAmount
		}

		result.Items = append(result.Items, item)
	}

	result.TotalBookQty = doc.TotalBookQuantity
	result.TotalActualQty = doc.TotalActualQuantity
	result.TotalSurplus = doc.TotalSurplusQuantity
	result.TotalShortage = doc.TotalShortageQuantity

	return result, nil
}

// ComparisonResult contains inventory comparison data.
type ComparisonResult struct {
	InventoryID    id.ID            `json:"inventoryId"`
	WarehouseID    id.ID            `json:"warehouseId"`
	Status         InventoryStatus  `json:"status"`
	Items          []ComparisonItem `json:"items"`
	TotalBookQty   types.Quantity   `json:"totalBookQty"`
	TotalActualQty types.Quantity   `json:"totalActualQty"`
	TotalSurplus   types.Quantity   `json:"totalSurplus"`
	TotalShortage  types.Quantity   `json:"totalShortage"`
}

// ComparisonItem represents a single comparison line.
type ComparisonItem struct {
	LineNo          int            `json:"lineNo"`
	ProductID       id.ID          `json:"productId"`
	BookQuantity    types.Quantity `json:"bookQuantity"`
	ActualQuantity  types.Quantity `json:"actualQuantity"`
	Deviation       types.Quantity `json:"deviation"`
	DeviationAmount int64          `json:"deviationAmount"`
	Counted         bool           `json:"counted"`
}
