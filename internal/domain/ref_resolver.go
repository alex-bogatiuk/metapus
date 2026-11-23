package domain

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
)

// RefResolveRequest represents a single typed reference to resolve.
type RefResolveRequest struct {
	RefType string `json:"refType" binding:"required"` // EntityName: "CryptoInvoice", "Counterparty"
	RefID   id.ID  `json:"refId"   binding:"required"` // UUID of the entity
}

// RefResolveResult represents a resolved typed reference with presentation.
type RefResolveResult struct {
	CurrencyCode  string `json:"currencyCode,omitempty"`
	DecimalPlaces *int   `json:"decimalPlaces,omitempty"`
	RefType       string `json:"refType"`      // EntityName
	RefID         id.ID  `json:"refId"`        // UUID
	Presentation  string `json:"presentation"` // e.g. "Крипто-инвойс CI-00042 от 15.03.2026"
	EntityType    string `json:"entityType"`   // "catalog" | "document"
	// Route is the entity's route prefix, for links such as /catalogs/{route}/{id}.
	Route string `json:"-"`

	// Document-specific resolution fields
	Number       string            `json:"number"`
	Date         time.Time         `json:"date"`
	Posted       bool              `json:"posted"`
	DeletionMark bool              `json:"deletionMark"`
	Amount       string            `json:"amount,omitempty"`
	CurrencyID   *id.ID            `json:"currencyId,omitempty"`
	PreviewData  map[string]string `json:"previewData,omitempty"` // label → resolved name
}

// RefResolver resolves typed references into human-readable presentations.
// Uses metadata registry to determine entity type and table, then queries DB.
type RefResolver interface {
	// ResolveRefs resolves a batch of typed references.
	// Returns deduplicated, accessible references in request order.
	// Missing, invalid and forbidden refs are identically absent.
	ResolveRefs(ctx context.Context, refs []RefResolveRequest) ([]RefResolveResult, error)
}
