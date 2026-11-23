package posting

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
)

// RegisterVisitor examines a Postable document and collects register-specific movements.
// Each register type provides its own visitor; configuration-specific
// registers keep their visitors next to the register, outside this package.
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
// Settlement register visitor
// ---------------------------------------------------------------------------

// SettlementMovementSource is implemented by documents that generate settlement movements.
// Documents opt-in to the settlement register by implementing this interface.
type SettlementMovementSource interface {
	GenerateSettlementMovements(ctx context.Context) ([]entity.SettlementMovement, error)
}

// SettlementVisitor collects settlement register movements from documents
// that implement SettlementMovementSource.
type SettlementVisitor struct{}

// Name implements RegisterVisitor.
func (v *SettlementVisitor) Name() string { return "settlement" }

// CollectMovements implements RegisterVisitor.
func (v *SettlementVisitor) CollectMovements(ctx context.Context, doc Postable, set *MovementSet) error {
	src, ok := doc.(SettlementMovementSource)
	if !ok {
		return nil
	}

	movements, err := src.GenerateSettlementMovements(ctx)
	if err != nil {
		return fmt.Errorf("generate settlement movements: %w", err)
	}

	set.SettlementMovements = append(set.SettlementMovements, movements...)
	return nil
}
