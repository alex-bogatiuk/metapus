package variants

import (
	"context"

	"github.com/google/uuid"

	"metapus/internal/core/apperror"
	corectx "metapus/internal/core/context"
)

// Service provides business logic for managing report variants.
type Service struct {
	repo Repository
}

// NewService creates a new variant service.
func NewService(repo Repository) *Service {
	return &Service{
		repo: repo,
	}
}

// Create creates a new variant.
func (s *Service) Create(ctx context.Context, variant *ReportVariant) error {
	user := corectx.GetUser(ctx)
	if user == nil {
		return apperror.NewUnauthorized("user not authenticated")
	}

	userID, err := uuid.Parse(user.UserID)
	if err != nil {
		return apperror.NewUnauthorized("invalid user ID format")
	}

	// Always assign current user as author unless it's a system variant created by admin
	if variant.Visibility != VisibilitySystem {
		variant.AuthorID = &userID
	}

	if err := variant.Validate(ctx); err != nil {
		return err
	}

	return s.repo.Create(ctx, variant)
}

// Update updates an existing variant.
func (s *Service) Update(ctx context.Context, variant *ReportVariant) error {
	user := corectx.GetUser(ctx)
	if user == nil {
		return apperror.NewUnauthorized("user not authenticated")
	}

	userID, err := uuid.Parse(user.UserID)
	if err != nil {
		return apperror.NewUnauthorized("invalid user ID format")
	}

	existing, err := s.repo.GetByID(ctx, variant.ID)
	if err != nil {
		return err
	}

	// Only author can update personal variants.
	// Only author or admin can update shared variants.
	if existing.Visibility == VisibilityPersonal && (existing.AuthorID == nil || *existing.AuthorID != userID) {
		return apperror.NewForbidden("cannot update another user's personal variant")
	}
	if existing.Visibility == VisibilitySystem {
		// Only admins should be able to update system variants (simplified check for now)
		// For now, prevent modifications to system variants via regular API
		return apperror.NewForbidden("cannot modify system variants")
	}

	// Carry over immutable fields from existing record
	variant.DatasetKey = existing.DatasetKey
	variant.AuthorID = existing.AuthorID

	if err := variant.Validate(ctx); err != nil {
		return err
	}

	return s.repo.Update(ctx, variant)
}

// Delete deletes a variant.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	user := corectx.GetUser(ctx)
	if user == nil {
		return apperror.NewUnauthorized("user not authenticated")
	}

	userID, err := uuid.Parse(user.UserID)
	if err != nil {
		return apperror.NewUnauthorized("invalid user ID format")
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if existing.Visibility == VisibilitySystem {
		return apperror.NewForbidden("cannot delete system variants")
	}
	if existing.Visibility == VisibilityPersonal && (existing.AuthorID == nil || *existing.AuthorID != userID) {
		return apperror.NewForbidden("cannot delete another user's personal variant")
	}

	return s.repo.Delete(ctx, id)
}

// GetList returns the list of variants accessible to the current user for a given dataset.
func (s *Service) GetList(ctx context.Context, datasetKey string) ([]*ReportVariant, error) {
	user := corectx.GetUser(ctx)
	if user == nil {
		return nil, apperror.NewUnauthorized("user not authenticated")
	}

	userID, err := uuid.Parse(user.UserID)
	if err != nil {
		return nil, apperror.NewUnauthorized("invalid user ID format")
	}

	return s.repo.GetList(ctx, datasetKey, userID)
}
