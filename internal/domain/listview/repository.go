package listview

import (
	"context"

	"github.com/google/uuid"
)

// Repository defines storage operations for list views.
type Repository interface {
	// Create inserts a new list view.
	Create(ctx context.Context, v *ListView) error

	// Update modifies an existing list view. Uses optimistic locking (version).
	Update(ctx context.Context, v *ListView) error

	// Delete soft-deletes a list view by ID.
	Delete(ctx context.Context, id uuid.UUID, version int) error

	// GetByID returns a single list view.
	GetByID(ctx context.Context, id uuid.UUID) (*ListView, error)

	// GetList returns views for an entity type accessible to the user:
	// system + shared + personal (only for this userID).
	GetList(ctx context.Context, entityType string, userID uuid.UUID) ([]*ListView, error)

	// ClearDefault clears other defaults in the selected view's visibility/author scope.
	// Must run in the same transaction as Create or Update.
	ClearDefault(ctx context.Context, v *ListView) error
}
