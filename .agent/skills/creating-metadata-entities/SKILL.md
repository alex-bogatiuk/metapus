---
name: creating-metadata-entities
description: Use when creating or modifying core metadata objects like Catalogs, Documents, or Registers. Ensures adherence to Clean Architecture, naming conventions, and metadata-driven principles.
---

# Creating Metadata Entities in Metapus

This skill guides you through the process of creating or modifying metadata entities (Catalogs, Documents, Registers) in the Metapus system, ensuring compliance with the "Code is Metadata" and Clean Architecture principles.

## Goal
To maintain a high-quality, consistent, and extensible codebase for business entities that follows the Metapus Manifesto.

## Core Principles
1. **CODE IS METADATA**: The Go struct is the source of truth.
2. **NAMING IS CONTRACT**: Use specific prefixes: `cat_`, `doc_`, `reg_`, `sys_`.
3. **LAYERED ISOLATION**: Keep Domain separate from Infrastructure and Presentation.

## Instructions

### 1. Directory Structure
All domain entities must reside in `internal/domain/`.
- Catalogs: `internal/domain/catalogs/[entity_name]/`
- Documents: `internal/domain/documents/[entity_name]/`
- Registers: `internal/domain/registers/[type]/[entity_name]/`

Each entity directory should typically contain:
- `model.go`: Struct definition and business validation (`Validate`).
- `repo.go`: Repository interface.
- `service.go`: Use case orchestration and posting logic.

### 2. Base Entity Usage
Always embed the correct base structure from `internal/core/entity`:
- **Catalogs**: Embed `entity.Catalog` (which embeds `entity.BaseCatalog`).
- **Documents**: Embed `entity.BaseDocument`.
- **Registers**: Use `entity.StockMovement` or `entity.StockBalance` for accumulation registers.

### 3. Validation
Implement the `entity.Validatable` interface in `model.go`. Validation should only check internal invariants.

### 4. Manifest Update
Every new entity must be documented in `Manifest.md` under the appropriate section.

## Examples

### Example: Creating a "Brand" Catalog

**model.go**:
```go
package brand

import (
    "context"
    "metapus/internal/core/entity"
)

type Brand struct {
    entity.Catalog
    Country string `db:"country" json:"country"`
}

func NewBrand(code, name, country string) *Brand {
    return &Brand{
        Catalog: entity.NewCatalog(code, name),
        Country: country,
    }
}

func (b *Brand) Validate(ctx context.Context) error {
    return b.Catalog.Validate(ctx)
}
```

## Constraints
- **NO** database access inside `Validate`.
- **NO** "tenant discriminator" columns in SQL or structs.
- **NO** third-party ORMs; use `pgx` and idiomatic Go.
