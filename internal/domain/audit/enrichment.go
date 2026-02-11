// Package audit provides utilities for audit field enrichment in domain entities.
package audit

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/core/security"
)

// getUserIDFromCtx extracts user ID from context and parses it as id.ID.
// Returns id.Nil() if user is not authenticated or ID is invalid.
func getUserIDFromCtx(ctx context.Context) id.ID {
	raw := security.GetUserID(ctx)
	if raw == "" {
		return id.Nil()
	}
	parsed, err := id.Parse(raw)
	if err != nil {
		return id.Nil()
	}
	return parsed
}

// EnrichCreatedBy sets CreatedBy and UpdatedBy fields from context user ID.
// Use in BeforeCreate hooks.
//
// The entity must implement SetCreatedBy(id.ID) and SetUpdatedBy(id.ID).
// If userID is not in context, this is a no-op.
func EnrichCreatedBy(ctx context.Context, entity interface{}) error {
	userID := getUserIDFromCtx(ctx)
	if id.IsNil(userID) {
		return nil
	}

	// Use type switch for known entity types
	// This avoids reflection and provides type safety
	switch e := entity.(type) {
	case interface {
		SetCreatedBy(id.ID)
		SetUpdatedBy(id.ID)
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
	userID := getUserIDFromCtx(ctx)
	if id.IsNil(userID) {
		return nil
	}

	switch e := entity.(type) {
	case interface{ SetUpdatedBy(id.ID) }:
		e.SetUpdatedBy(userID)
	default:
		// Fallback for entities with public fields
	}

	return nil
}

// EnrichCreatedByDirect is a type-safe helper that sets fields directly.
// Use when entity has public CreatedBy/UpdatedBy id.ID fields.
func EnrichCreatedByDirect(ctx context.Context, createdBy, updatedBy *id.ID) {
	userID := getUserIDFromCtx(ctx)
	if !id.IsNil(userID) && createdBy != nil && updatedBy != nil {
		*createdBy = userID
		*updatedBy = userID
	}
}

// EnrichUpdatedByDirect is a type-safe helper that sets UpdatedBy field directly.
func EnrichUpdatedByDirect(ctx context.Context, updatedBy *id.ID) {
	userID := getUserIDFromCtx(ctx)
	if !id.IsNil(userID) && updatedBy != nil {
		*updatedBy = userID
	}
}
