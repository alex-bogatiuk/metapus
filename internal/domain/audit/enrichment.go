// Package audit provides utilities for audit field enrichment in domain entities.
package audit

import (
	"context"

	"metapus/internal/core/security"
)

// EnrichCreatedBy sets CreatedBy and UpdatedBy fields from context user ID.
// Use in BeforeCreate hooks.
//
// The entity must have public CreatedBy and UpdatedBy string fields.
// If userID is not in context, this is a no-op.
func EnrichCreatedBy(ctx context.Context, entity interface{}) error {
	userID := security.GetUserID(ctx)
	if userID == "" {
		return nil
	}

	// Use type switch for known entity types
	// This avoids reflection and provides type safety
	switch e := entity.(type) {
	case interface {
		SetCreatedBy(string)
		SetUpdatedBy(string)
	}:
		e.SetCreatedBy(userID)
		e.SetUpdatedBy(userID)
	default:
		// If entity has public fields, set directly via reflection fallback
		// For now, we'll handle this per-entity type
	}

	return nil
}

// EnrichUpdatedBy sets only UpdatedBy field from context user ID.
// Use in BeforeUpdate hooks.
//
// If userID is not in context, this is a no-op.
func EnrichUpdatedBy(ctx context.Context, entity interface{}) error {
	userID := security.GetUserID(ctx)
	if userID == "" {
		return nil
	}

	switch e := entity.(type) {
	case interface{ SetUpdatedBy(string) }:
		e.SetUpdatedBy(userID)
	default:
		// Fallback for entities with public fields
	}

	return nil
}

// EnrichCreatedByDirect is a type-safe helper that sets fields directly.
// Use when entity has public CreatedBy/UpdatedBy fields.
func EnrichCreatedByDirect(ctx context.Context, createdBy, updatedBy *string) {
	userID := security.GetUserID(ctx)
	if userID != "" && createdBy != nil && updatedBy != nil {
		*createdBy = userID
		*updatedBy = userID
	}
}

// EnrichUpdatedByDirect is a type-safe helper that sets UpdatedBy field directly.
func EnrichUpdatedByDirect(ctx context.Context, updatedBy *string) {
	userID := security.GetUserID(ctx)
	if userID != "" && updatedBy != nil {
		*updatedBy = userID
	}
}
