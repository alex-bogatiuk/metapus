package settings

import (
	"context"
	"encoding/json"
)

// Repository defines storage operations for tenant-level system settings.
type Repository interface {
	// Get returns the current settings. If no row exists, returns defaults.
	Get(ctx context.Context) (*Settings, error)

	// UpdateSection atomically updates a single JSONB section with optimistic locking.
	// section must be one of: "organization", "accounting", "performance".
	// version is the expected current version (for conflict detection).
	// Returns the updated settings on success.
	UpdateSection(ctx context.Context, section string, data json.RawMessage, version int) (*Settings, error)
}
