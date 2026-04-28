// Package listview provides domain logic for saved list views (filter presets).
// A list view is a named combination of filters, visible columns, and sort order
// that users can save and restore for any entity list page.
package listview

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"metapus/internal/core/apperror"
)

// Visibility defines who can see a list view.
type Visibility string

const (
	VisibilityPersonal Visibility = "personal"
	VisibilityShared   Visibility = "shared"
	VisibilitySystem   Visibility = "system"
)

// Config holds the saved state of a list view.
// Filters are opaque JSON — the frontend owns the schema.
type Config struct {
	Filters    json.RawMessage `json:"filters"`              // FilterValues, frontend-owned schema
	Columns    []string        `json:"columns,omitempty"`    // visible column keys
	SortColumn *string         `json:"sortColumn,omitempty"` // sort column key
	SortDir    string          `json:"sortDir,omitempty"`    // "asc" | "desc"
}

// ListView represents a saved list configuration for an entity type.
type ListView struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	EntityType   string     `json:"entityType" db:"entity_type"`
	Name         string     `json:"name" db:"name"`
	AuthorID     *uuid.UUID `json:"authorId" db:"author_id"`
	Visibility   Visibility `json:"visibility" db:"visibility"`
	IsDefault    bool       `json:"isDefault" db:"is_default"`
	SortOrder    int        `json:"sortOrder" db:"sort_order"`
	Config       Config     `json:"config" db:"config"`
	DeletionMark bool       `json:"deletionMark" db:"deletion_mark"`
	Version      int        `json:"version" db:"version"`
	CreatedAt    time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time  `json:"updatedAt" db:"updated_at"`
}

// Validate checks basic integrity of the list view. Pure function, no DB calls.
func (v *ListView) Validate(_ context.Context) error {
	var err *apperror.AppError

	if v.EntityType == "" {
		err = apperror.NewValidation("validation failed").WithDetail("entityType", "required")
	}
	if v.Name == "" {
		if err == nil {
			err = apperror.NewValidation("validation failed")
		}
		err = err.WithDetail("name", "required")
	}
	if v.Visibility != VisibilityPersonal && v.Visibility != VisibilityShared && v.Visibility != VisibilitySystem {
		if err == nil {
			err = apperror.NewValidation("validation failed")
		}
		err = err.WithDetail("visibility", "invalid visibility type")
	}
	if v.Visibility == VisibilityPersonal && v.AuthorID == nil {
		if err == nil {
			err = apperror.NewValidation("validation failed")
		}
		err = err.WithDetail("authorId", "personal views must have an author")
	}

	if err != nil {
		return err
	}
	return nil
}
