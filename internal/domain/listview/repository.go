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
	Delete(ctx context.Context, id uuid.UUID) error

	// GetByID returns a single list view.
	GetByID(ctx context.Context, id uuid.UUID) (*ListView, error)

	// GetList returns views for an entity type accessible to the user:
	// system + shared + personal (only for this userID).
	GetList(ctx context.Context, entityType string, userID uuid.UUID) ([]*ListView, error)

	// ClearDefault resets is_default=false for all views of an entity type
	// belonging to the user (personal) or shared/system scope.
	ClearDefault(ctx context.Context, entityType string, userID uuid.UUID) error
}
