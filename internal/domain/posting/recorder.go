// Package posting — recorder.go provides the RegisterRecorder abstraction.
// RegisterRecorder decouples the Engine from concrete register services,
// enabling client extensions to add new register types without modifying core.
package posting

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/settlement"
)

// RegisterRecorder handles recording and reversal of movements for one register type.
// Each register (settlement, custom) implements this interface.
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

	// ReverseMovements reverses a document version. Append-only ledgers append
	// opposite entries rather than deleting their original movements.
	ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error
}

// PostingValidator is an optional interface for recorders that need to validate
// constraints before recording movements (e.g., balance availability check).
// If a RegisterRecorder also implements PostingValidator, the Engine calls
// ValidateBeforePost after collecting movements but before recording.
type PostingValidator interface {
	ValidateBeforePost(ctx context.Context, set *MovementSet) error
}

// PostingResourceLocker runs before evidence validators, balances and old
// movement reversals. It locks a complete resource set, without writing money.
type PostingResourceLocker interface {
	LockPostingResources(context.Context, Postable) error
	LockReversalResources(context.Context, id.ID) error
}

// ---------------------------------------------------------------------------
// Built-in recorders (adapters over existing register services)
// ---------------------------------------------------------------------------

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

// DefaultRecorders returns the built-in register recorders for settlement.
// Use this when constructing the default Engine:
//
//	recorders := posting.DefaultRecorders(settlementService)
//	engine := posting.NewEngine(docLocker, recorders...)
func DefaultRecorders(
	settlementService *settlement.Service,
) []RegisterRecorder {
	return []RegisterRecorder{
		NewSettlementRecorder(settlementService),
	}
}
