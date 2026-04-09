package domain

import (
	"context"

	"metapus/internal/core/id"
)

// FindReferencesRequest specifies which entity to search references for.
type FindReferencesRequest struct {
	EntityName string `json:"entityName" binding:"required"` // "Counterparty", "GoodsReceipt"
	EntityID   id.ID  `json:"entityId"   binding:"required"` // UUID
}

// FoundReference represents a single incoming reference to the target entity.
type FoundReference struct {
	// Source entity that references the target
	SourceEntityName string `json:"sourceEntityName"` // e.g. "GoodsReceipt"
	SourceEntityType string `json:"sourceEntityType"` // "catalog" | "document"
	SourceField      string `json:"sourceField"`      // field name, e.g. "counterpartyId" or "lines.ref"
	SourceID         id.ID  `json:"sourceId"`         // ID of the referencing entity
	Presentation     string `json:"presentation"`     // resolved display string
}

// RefFinder searches for all references to a given entity across all registered tables.
// Analogous to 1C's "Найти ссылки на объект".
type RefFinder interface {
	// FindReferences searches all registered entity tables for references to the target.
	// Scans both TypedRef fields (ref_type + ref_id) and standard FK reference fields.
	FindReferences(ctx context.Context, req FindReferencesRequest) ([]FoundReference, error)

	// CountReferences returns the number of references to the target (fast path for delete check).
	CountReferences(ctx context.Context, req FindReferencesRequest) (int, error)
}
