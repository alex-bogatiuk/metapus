package platform

// Re-export domain service types for client extensions.
// Extensions should use platform.CatalogService etc. instead of
// importing "metapus/internal/domain" directly.

import (
	"metapus/internal/core/entity"
	"metapus/internal/domain"
)

// ── Type Constraints ────────────────────────────────────────────────────

// Validatable is the constraint for entities that can validate themselves.
type Validatable = entity.Validatable

// ── Service Types ───────────────────────────────────────────────────────

// CatalogService is the generic service for catalog CRUD operations.
type CatalogService[T entity.Validatable] = domain.CatalogService[T]

// CatalogServiceConfig configures a CatalogService instance.
type CatalogServiceConfig[T entity.Validatable] = domain.CatalogServiceConfig[T]

// CatalogRepository is the generic repository interface for catalogs.
type CatalogRepository[T entity.Validatable] = domain.CatalogRepository[T]
