// Package posting — recorder.go provides the RegisterRecorder abstraction.
// RegisterRecorder decouples the Engine from concrete register services,
// enabling client extensions to add new register types without modifying core.
package posting

import (
	"bytes"
	"context"
	"sort"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/registers/cost"
	"metapus/internal/domain/registers/settlement"
	"metapus/internal/domain/registers/stock"
)

// RegisterRecorder handles recording and reversal of movements for one register type.
// Each register (stock, cost, settlement, custom) implements this interface.
//
// Adding a new register:
//  1. Create movement entity, repo, service in internal/domain/registers/<name>/
//  2. Create a Visitor that collects movements into MovementSet.Extensions["<name>"]
//  3. Implement RegisterRecorder that reads from Extensions and delegates to service.
//  4. Pass to Engine via NewEngine(docLocker, recorders...) or Engine.AddRecorder().
type RegisterRecorder interface {
	// Name returns the register name for logging/debugging.
	Name() string

	// RecordFromSet extracts and records relevant movements from the set.
	// Implementations should no-op if there are no movements of their type.
	RecordFromSet(ctx context.Context, set *MovementSet) error

	// ReverseMovements deletes movements for a given document version.
	ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error
}

// PostingValidator is an optional interface for recorders that need to validate
// constraints before recording movements (e.g., stock availability check).
// If a RegisterRecorder also implements PostingValidator, the Engine calls
// ValidateBeforePost after collecting movements but before recording.
type PostingValidator interface {
	ValidateBeforePost(ctx context.Context, set *MovementSet) error
}

// ---------------------------------------------------------------------------
// Built-in recorders (adapters over existing register services)
// ---------------------------------------------------------------------------

// StockRecorder adapts stock.Service into a RegisterRecorder.
// Also implements PostingValidator for stock availability checks.
type StockRecorder struct {
	service *stock.Service
}

// NewStockRecorder creates a new StockRecorder.
func NewStockRecorder(s *stock.Service) *StockRecorder {
	return &StockRecorder{service: s}
}

func (r *StockRecorder) Name() string { return "stock" }

func (r *StockRecorder) RecordFromSet(ctx context.Context, set *MovementSet) error {
	if len(set.StockMovements) == 0 {
		return nil
	}
	return r.service.RecordMovements(ctx, set.StockMovements)
}

func (r *StockRecorder) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.service.ReverseMovements(ctx, recorderID, beforeVersion)
}

// ValidateBeforePost implements PostingValidator — checks stock availability
// for expense movements with resource ordering to prevent deadlocks.
func (r *StockRecorder) ValidateBeforePost(ctx context.Context, set *MovementSet) error {
	return validateStockAvailability(r.service, ctx, set.StockMovements)
}

// CostRecorder adapts cost.Service into a RegisterRecorder.
type CostRecorder struct {
	service *cost.Service
}

// NewCostRecorder creates a new CostRecorder.
func NewCostRecorder(s *cost.Service) *CostRecorder {
	return &CostRecorder{service: s}
}

func (r *CostRecorder) Name() string { return "cost" }

func (r *CostRecorder) RecordFromSet(ctx context.Context, set *MovementSet) error {
	if len(set.CostMovements) == 0 {
		return nil
	}
	return r.service.RecordMovements(ctx, set.CostMovements)
}

func (r *CostRecorder) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.service.ReverseMovements(ctx, recorderID, beforeVersion)
}

// SettlementRecorder adapts settlement.Service into a RegisterRecorder.
type SettlementRecorder struct {
	service *settlement.Service
}

// NewSettlementRecorder creates a new SettlementRecorder.
func NewSettlementRecorder(s *settlement.Service) *SettlementRecorder {
	return &SettlementRecorder{service: s}
}

func (r *SettlementRecorder) Name() string { return "settlement" }

func (r *SettlementRecorder) RecordFromSet(ctx context.Context, set *MovementSet) error {
	if len(set.SettlementMovements) == 0 {
		return nil
	}
	return r.service.RecordMovements(ctx, set.SettlementMovements)
}

func (r *SettlementRecorder) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.service.ReverseMovements(ctx, recorderID, beforeVersion)
}

// DefaultRecorders returns the built-in register recorders for stock, cost, and settlement.
// Use this when constructing the default Engine:
//
//	recorders := posting.DefaultRecorders(stockService, costService, settlementService)
//	engine := posting.NewEngine(docLocker, recorders...)
func DefaultRecorders(
	stockService *stock.Service,
	costService *cost.Service,
	settlementService *settlement.Service,
) []RegisterRecorder {
	return []RegisterRecorder{
		NewStockRecorder(stockService),
		NewCostRecorder(costService),
		NewSettlementRecorder(settlementService),
	}
}

// stockDimKey is a composite map key for grouping movements by warehouse+product.
// Uses struct key instead of string concatenation — consistent with dimKey in CheckAndReserveStock.
type stockDimKey struct {
	warehouseID, productID id.ID
}

// validateStockAvailability checks if there's enough stock for expense movements.
// Extracted as a package-level function used by StockRecorder.
func validateStockAvailability(stockService *stock.Service, ctx context.Context, movements []entity.StockMovement) error {
	reserves := make(map[stockDimKey]*stock.StockReservation)

	for _, m := range movements {
		if m.RecordType != entity.RecordTypeExpense {
			continue
		}

		key := stockDimKey{m.WarehouseID, m.ProductID}
		if existing, ok := reserves[key]; ok {
			existing.RequiredQty += m.Quantity
		} else {
			reserves[key] = &stock.StockReservation{
				WarehouseID: m.WarehouseID,
				ProductID:   m.ProductID,
				RequiredQty: m.Quantity,
			}
		}
	}

	if len(reserves) == 0 {
		return nil
	}

	items := make([]stock.StockReservation, 0, len(reserves))
	for _, r := range reserves {
		items = append(items, *r)
	}

	// IMPORTANT: Lock balances in deterministic order to prevent deadlocks.
	// Uses bytes.Compare on raw UUID bytes — avoids string allocation per comparison.
	sort.Slice(items, func(i, j int) bool {
		if c := bytes.Compare(items[i].WarehouseID[:], items[j].WarehouseID[:]); c != 0 {
			return c < 0
		}
		return bytes.Compare(items[i].ProductID[:], items[j].ProductID[:]) < 0
	})

	return stockService.CheckAndReserveStock(ctx, items)
}
