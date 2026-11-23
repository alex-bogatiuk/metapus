// Package audit fills the created-by and updated-by fields.
package audit

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	core "github.com/alex-bogatiuk/metapus/internal/domain/audit"
)

// EnrichCreatedByDirect is a type-safe helper that sets fields directly.
// Use when entity has public CreatedBy/UpdatedBy id.ID fields.
func EnrichCreatedByDirect(ctx context.Context, createdBy *id.ID, updatedBy *id.ID) {
	core.EnrichCreatedByDirect(ctx, createdBy, updatedBy)
}

// EnrichUpdatedByDirect is a type-safe helper that sets UpdatedBy field directly.
func EnrichUpdatedByDirect(ctx context.Context, updatedBy *id.ID) {
	core.EnrichUpdatedByDirect(ctx, updatedBy)
}
