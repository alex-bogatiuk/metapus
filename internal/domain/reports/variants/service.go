package variants

import (
	"context"
	"maps"
	"slices"

	"github.com/google/uuid"

	"metapus/internal/core/apperror"
	corectx "metapus/internal/core/context"
	"metapus/internal/domain/viewpolicy"
)

type Service struct {
	repo               Repository
	datasetPermissions map[string]string
}

func NewService(repo Repository, datasetPermissions map[string]string) *Service {
	return &Service{repo: repo, datasetPermissions: maps.Clone(datasetPermissions)}
}

func (s *Service) requireDataset(ctx context.Context, key string) error {
	user := corectx.GetUser(ctx)
	if user == nil {
		return apperror.NewUnauthorized("authentication required")
	}
	permission, ok := s.datasetPermissions[key]
	if !ok || permission == "" {
		return apperror.NewValidation("unknown report dataset").WithDetail("datasetKey", "invalid dataset")
	}
	if !user.IsAdmin && !slices.Contains(user.Permissions, permission) {
		return apperror.NewForbidden("insufficient permissions").WithDetail("required_permission", permission)
	}
	return nil
}

func (s *Service) Create(ctx context.Context, variant *ReportVariant) error {
	userID, err := viewpolicy.RequireCreate(ctx, string(variant.Visibility))
	if err != nil {
		return err
	}
	if err := s.requireDataset(ctx, variant.DatasetKey); err != nil {
		return err
	}
	variant.AuthorID = viewpolicy.Author(string(variant.Visibility), nil, userID)
	if err := variant.Validate(ctx); err != nil {
		return err
	}
	return s.repo.Create(ctx, variant)
}

func (s *Service) Update(ctx context.Context, variant *ReportVariant) error {
	if _, err := viewpolicy.UserID(ctx); err != nil {
		return err
	}
	existing, err := s.repo.GetByID(ctx, variant.ID)
	if err != nil {
		return err
	}
	if err := s.requireDataset(ctx, existing.DatasetKey); err != nil {
		return err
	}
	userID, err := viewpolicy.RequireEdit(ctx, string(existing.Visibility), existing.AuthorID)
	if err != nil {
		return err
	}
	if _, err := viewpolicy.RequireCreate(ctx, string(variant.Visibility)); err != nil {
		return err
	}
	variant.DatasetKey = existing.DatasetKey
	variant.AuthorID = viewpolicy.Author(string(variant.Visibility), existing.AuthorID, userID)
	if err := variant.Validate(ctx); err != nil {
		return err
	}
	return s.repo.Update(ctx, variant)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := viewpolicy.UserID(ctx); err != nil {
		return err
	}
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.requireDataset(ctx, existing.DatasetKey); err != nil {
		return err
	}
	if _, err := viewpolicy.RequireEdit(ctx, string(existing.Visibility), existing.AuthorID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id, existing.Version)
}

func (s *Service) GetList(ctx context.Context, datasetKey string) ([]*ReportVariant, error) {
	userID, err := viewpolicy.UserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireDataset(ctx, datasetKey); err != nil {
		return nil, err
	}
	return s.repo.GetList(ctx, datasetKey, userID)
}
