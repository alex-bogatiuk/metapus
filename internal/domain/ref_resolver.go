package domain

import (
	"context"
	"time"

	"metapus/internal/core/id"
)

// RefResolveRequest represents a single typed reference to resolve.
type RefResolveRequest struct {
	RefType string `json:"refType" binding:"required"` // EntityName: "GoodsReceipt", "Counterparty"
	RefID   id.ID  `json:"refId"   binding:"required"` // UUID of the entity
}

// RefResolveResult represents a resolved typed reference with presentation.
type RefResolveResult struct {
	RefType      string `json:"refType"`      // EntityName
	RefID        id.ID  `json:"refId"`        // UUID
	Presentation string `json:"presentation"` // e.g. "Поступление товаров ПТ-00042 от 15.03.2026"
	EntityType   string `json:"entityType"`   // "catalog" | "document"
	
	// Document-specific resolution fields
	Number       string    `json:"number"`
	Date         time.Time `json:"date"`
	Posted       bool      `json:"posted"`
	DeletionMark bool      `json:"deletionMark"`
	Amount       int64             `json:"amount,omitempty"`
	CurrencyID   *id.ID            `json:"currencyId,omitempty"`
	PreviewData  map[string]string `json:"previewData,omitempty"` // label → resolved name
}

// RefResolver resolves typed references into human-readable presentations.
// Uses metadata registry to determine entity type and table, then queries DB.
type RefResolver interface {
	// ResolveRefs resolves a batch of typed references.
	// Returns results in the same order as requests.
	// Missing/invalid refs return empty presentation (no error).
	ResolveRefs(ctx context.Context, refs []RefResolveRequest) ([]RefResolveResult, error)
}
