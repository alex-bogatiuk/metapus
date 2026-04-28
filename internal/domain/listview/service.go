package listview

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"metapus/internal/core/apperror"
	corectx "metapus/internal/core/context"
)

// Service provides business logic for managing list views.
type Service struct {
	repo Repository
}

// NewService creates a new list view service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Create creates a new list view. Assigns the current user as author for non-system views.
func (s *Service) Create(ctx context.Context, v *ListView) error {
	userID, err := s.requireUserID(ctx)
	if err != nil {
		return err
	}

	if v.Visibility != VisibilitySystem {
		v.AuthorID = &userID
	}

	if err := v.Validate(ctx); err != nil {
		return err
	}

	// If this view is marked as default, clear existing defaults first.
	if v.IsDefault {
		if err := s.repo.ClearDefault(ctx, v.EntityType, userID); err != nil {
			return fmt.Errorf("clear default: %w", err)
		}
	}

	return s.repo.Create(ctx, v)
}

// Update updates an existing list view with ownership check.
func (s *Service) Update(ctx context.Context, v *ListView) error {
	userID, err := s.requireUserID(ctx)
	if err != nil {
		return err
	}

	existing, err := s.repo.GetByID(ctx, v.ID)
	if err != nil {
		return err
	}

	if existing.Visibility == VisibilityPersonal && (existing.AuthorID == nil || *existing.AuthorID != userID) {
		return apperror.NewForbidden("cannot update another user's personal view")
	}
	if existing.Visibility == VisibilitySystem {
		return apperror.NewForbidden("cannot modify system views")
	}

	// Carry over immutable fields from existing record.
	v.EntityType = existing.EntityType
	v.AuthorID = existing.AuthorID

	if err := v.Validate(ctx); err != nil {
		return err
	}

	if v.IsDefault {
		if err := s.repo.ClearDefault(ctx, v.EntityType, userID); err != nil {
			return fmt.Errorf("clear default: %w", err)
		}
	}

	return s.repo.Update(ctx, v)
}

// Delete soft-deletes a list view with ownership check.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	userID, err := s.requireUserID(ctx)
	if err != nil {
		return err
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if existing.Visibility == VisibilitySystem {
		return apperror.NewForbidden("cannot delete system views")
	}
	if existing.Visibility == VisibilityPersonal && (existing.AuthorID == nil || *existing.AuthorID != userID) {
		return apperror.NewForbidden("cannot delete another user's personal view")
	}

	return s.repo.Delete(ctx, id)
}

// GetList returns views for an entity type accessible to the current user.
func (s *Service) GetList(ctx context.Context, entityType string) ([]*ListView, error) {
	userID, err := s.requireUserID(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.GetList(ctx, entityType, userID)
}

// SetDefault marks a view as default for the current user,
// clearing the previous default for the same entity type.
func (s *Service) SetDefault(ctx context.Context, id uuid.UUID) error {
	userID, err := s.requireUserID(ctx)
	if err != nil {
		return err
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.repo.ClearDefault(ctx, existing.EntityType, userID); err != nil {
		return fmt.Errorf("clear default: %w", err)
	}

	existing.IsDefault = true
	return s.repo.Update(ctx, existing)
}

// requireUserID extracts and parses user ID from context.
func (s *Service) requireUserID(ctx context.Context) (uuid.UUID, error) {
	user := corectx.GetUser(ctx)
	if user == nil {
		return uuid.UUID{}, apperror.NewUnauthorized("user not authenticated")
	}

	userID, err := uuid.Parse(user.UserID)
	if err != nil {
		return uuid.UUID{}, apperror.NewUnauthorized("invalid user ID format")
	}

	return userID, nil
}
