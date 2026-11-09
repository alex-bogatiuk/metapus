package platform

// Re-export core entity types for client extensions.
// Extensions should import "metapus/internal/platform" instead of
// "metapus/internal/core/entity" or "metapus/internal/core/id" directly.
//
// This provides a stable import surface — internal package paths may change,
// but platform/ re-exports remain stable.

import (
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// ── Entity Types ────────────────────────────────────────────────────────

// Catalog is the base type for all catalog (reference) entities.
type Catalog = entity.Catalog

// Document is the base type for all document entities.
type Document = entity.Document

// Attributes holds custom JSONB fields (extension fields).
type Attributes = entity.Attributes

// ── ID Type ─────────────────────────────────────────────────────────────

// ID is the primary identifier type (UUIDv7).
type ID = id.ID

// ParseID parses a string into an ID.
var ParseID = id.Parse

// NewID generates a new UUIDv7 ID.
var NewID = id.New

// ── Entity Constructors ─────────────────────────────────────────────────

// NewCatalog creates a new catalog entity with the given code and name.
var NewCatalog = entity.NewCatalog
