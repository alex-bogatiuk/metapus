package posting

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
)

// RegisterVisitor examines a Postable document and collects register-specific movements.
// Each register type (stock, cost, settlement, …) provides its own visitor.
//
// The Engine iterates all registered visitors during posting.
// Adding a new register = create a new source interface + visitor implementation,
// then register it via Engine.AddVisitor(). No existing document changes needed.
type RegisterVisitor interface {
	// Name returns the register name for logging/debugging.
	Name() string

	// CollectMovements examines the document and appends movements to the set.
	// If the document doesn't implement the corresponding source interface,
	// the visitor must return nil (skip silently).
	CollectMovements(ctx context.Context, doc Postable, set *MovementSet) error
}

// ---------------------------------------------------------------------------
// Stock register visitor
// ---------------------------------------------------------------------------

// StockMovementSource is implemented by documents that generate stock register movements.
// Documents opt-in to the stock register by implementing this interface.
//
// Example (GoodsReceipt):
//
//	func (g *GoodsReceipt) GenerateStockMovements(ctx context.Context) ([]entity.StockMovement, error) {
//	    // create receipt movements per line …
//	}
type StockMovementSource interface {
	GenerateStockMovements(ctx context.Context) ([]entity.StockMovement, error)
}

// StockVisitor collects stock register movements from documents
// that implement StockMovementSource.
type StockVisitor struct{}

// Name implements RegisterVisitor.
func (v *StockVisitor) Name() string { return "stock" }

// CollectMovements implements RegisterVisitor.
// If the document does not implement StockMovementSource, it is silently skipped.
func (v *StockVisitor) CollectMovements(ctx context.Context, doc Postable, set *MovementSet) error {
	src, ok := doc.(StockMovementSource)
	if !ok {
		return nil // document doesn't generate stock movements
	}

	movements, err := src.GenerateStockMovements(ctx)
	if err != nil {
		return fmt.Errorf("generate stock movements: %w", err)
	}

	set.StockMovements = append(set.StockMovements, movements...)
	return nil
}

// ---------------------------------------------------------------------------
// Future visitors (uncomment when registers are implemented)
// ---------------------------------------------------------------------------

// CostMovementSource is implemented by documents that generate cost register movements.
// type CostMovementSource interface {
//     GenerateCostMovements(ctx context.Context) ([]entity.CostMovement, error)
// }

// SettlementMovementSource is implemented by documents that generate settlement movements.
// type SettlementMovementSource interface {
//     GenerateSettlementMovements(ctx context.Context) ([]entity.SettlementMovement, error)
// }
