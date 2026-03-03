package userpref

import (
	"context"
	"encoding/json"

	"metapus/internal/core/id"
)

// Repository defines storage operations for user preferences.
type Repository interface {
	// GetOrCreate returns preferences for the user, creating an empty row if absent.
	GetOrCreate(ctx context.Context, userID id.ID) (*UserPreferences, error)

	// SaveInterface atomically upserts the interface section.
	SaveInterface(ctx context.Context, userID id.ID, prefs InterfacePrefs) error

	// SaveListFilters upserts filters for a single entity type (JSONB merge).
	SaveListFilters(ctx context.Context, userID id.ID, entityType string, data json.RawMessage) error

	// SaveListColumns upserts visible columns for a single entity type (JSONB merge).
	SaveListColumns(ctx context.Context, userID id.ID, entityType string, data json.RawMessage) error
}
