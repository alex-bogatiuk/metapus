package variants

import (
	"context"
	"time"

	"github.com/google/uuid"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/filter"
)

// Visibility defines who can see a report variant.
type Visibility string

const (
	VisibilityPersonal Visibility = "personal"
	VisibilityShared   Visibility = "shared"
	VisibilitySystem   Visibility = "system"
)

// ReportVariant represents a saved configuration for a report.
type ReportVariant struct {
	ID         uuid.UUID     `json:"id" db:"id"`
	DatasetKey string        `json:"datasetKey" db:"dataset_key"`
	Name       string        `json:"name" db:"name"`
	AuthorID   *uuid.UUID    `json:"authorId" db:"author_id"` // NULL if system
	Visibility Visibility    `json:"visibility" db:"visibility"`
	IsDefault  bool          `json:"isDefault" db:"is_default"`
	Config     VariantConfig `json:"config" db:"config"`

	DeletionMark bool      `json:"deletionMark" db:"deletion_mark"`
	Version      int       `json:"version" db:"version"`
	CreatedAt    time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time `json:"updatedAt" db:"updated_at"`
}

// VariantConfig holds the JSON configuration of the variant.
type VariantConfig struct {
	SelectedFields  []string       `json:"selectedFields"`
	VisibleColumns  []string       `json:"visibleColumns"`
	GroupBy         []string       `json:"groupBy"`
	SortColumn      *string        `json:"sortColumn"`
	SortDirection   string         `json:"sortDirection"`
	Filters         map[string]any `json:"filters"`
	AdvancedFilters []filter.Item  `json:"advancedFilters"`
}

// Validate checks the basic integrity of the report variant.
func (r *ReportVariant) Validate(ctx context.Context) error {
	var err *apperror.AppError

	if r.DatasetKey == "" {
		err = apperror.NewValidation("validation failed").WithDetail("datasetKey", "required")
	}
	if r.Name == "" {
		if err == nil {
			err = apperror.NewValidation("validation failed")
		}
		err = err.WithDetail("name", "required")
	}
	if r.Visibility != VisibilityPersonal && r.Visibility != VisibilityShared && r.Visibility != VisibilitySystem {
		if err == nil {
			err = apperror.NewValidation("validation failed")
		}
		err = err.WithDetail("visibility", "invalid visibility type")
	}

	if r.Visibility == VisibilityPersonal && r.AuthorID == nil {
		if err == nil {
			err = apperror.NewValidation("validation failed")
		}
		err = err.WithDetail("authorId", "personal variants must have an author")
	}

	if err != nil {
		return err
	}
	return nil
}
