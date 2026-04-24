package variants

import (
	"context"

	"github.com/google/uuid"
)

// Repository defines the interface for persisting report variants.
type Repository interface {
	Create(ctx context.Context, variant *ReportVariant) error
	Update(ctx context.Context, variant *ReportVariant) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*ReportVariant, error)
	GetList(ctx context.Context, datasetKey string, userID uuid.UUID) ([]*ReportVariant, error)
}
