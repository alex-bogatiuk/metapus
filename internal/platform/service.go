package platform

// Re-export domain service types for client extensions.
// Extensions should use platform.CatalogService etc. instead of
// importing "github.com/alex-bogatiuk/metapus/internal/domain" directly.

import (
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/domain"
)

// ── Type Constraints ────────────────────────────────────────────────────

// CatalogEntity is the constraint for catalog entities.
type CatalogEntity = entity.CatalogEntity

// Validatable is the constraint for entities that can validate themselves.
type Validatable = entity.Validatable

// ── Service Types ───────────────────────────────────────────────────────

// CatalogService is the generic service for catalog CRUD operations.
type CatalogService[T entity.CatalogEntity] = domain.CatalogService[T]

// CatalogServiceConfig configures a CatalogService instance.
type CatalogServiceConfig[T entity.CatalogEntity] = domain.CatalogServiceConfig[T]

// CatalogRepository is the generic repository interface for catalogs.
type CatalogRepository[T entity.CatalogEntity] = domain.CatalogRepository[T]
