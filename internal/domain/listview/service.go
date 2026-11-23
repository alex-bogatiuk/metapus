package listview

import (
	"context"

	"github.com/google/uuid"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/domain/viewpolicy"
)

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Create(ctx context.Context, v *ListView) error {
	userID, err := viewpolicy.RequireCreate(ctx, string(v.Visibility))
	if err != nil {
		return err
	}
	v.AuthorID = viewpolicy.Author(string(v.Visibility), nil, userID)
	if err := v.Validate(ctx); err != nil {
		return err
	}
	return s.save(ctx, v, s.repo.Create)
}

func (s *Service) Update(ctx context.Context, v *ListView) error {
	if _, err := viewpolicy.UserID(ctx); err != nil {
		return err
	}
	existing, err := s.repo.GetByID(ctx, v.ID)
	if err != nil {
		return err
	}
	userID, err := viewpolicy.RequireEdit(ctx, string(existing.Visibility), existing.AuthorID)
	if err != nil {
		return err
	}
	if _, err := viewpolicy.RequireCreate(ctx, string(v.Visibility)); err != nil {
		return err
	}
	v.EntityType = existing.EntityType
	v.AuthorID = viewpolicy.Author(string(v.Visibility), existing.AuthorID, userID)
	if err := v.Validate(ctx); err != nil {
		return err
	}
	return s.save(ctx, v, s.repo.Update)
}

// Default changes and the selected view must commit together, including on a
// version conflict. Clearing a default never touches another author's scope.
func (s *Service) save(ctx context.Context, v *ListView, persist func(context.Context, *ListView) error) error {
	if !v.IsDefault {
		return persist(ctx, v)
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err)
	}
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.ClearDefault(txCtx, v); err != nil {
			return err
		}
		return persist(txCtx, v)
	})
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := viewpolicy.UserID(ctx); err != nil {
		return err
	}
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := viewpolicy.RequireEdit(ctx, string(existing.Visibility), existing.AuthorID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id, existing.Version)
}

func (s *Service) GetList(ctx context.Context, entityType string) ([]*ListView, error) {
	userID, err := viewpolicy.UserID(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.GetList(ctx, entityType, userID)
}

func (s *Service) SetDefault(ctx context.Context, id uuid.UUID) error {
	if _, err := viewpolicy.UserID(ctx); err != nil {
		return err
	}
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := viewpolicy.RequireEdit(ctx, string(existing.Visibility), existing.AuthorID); err != nil {
		return err
	}
	existing.IsDefault = true
	return s.save(ctx, existing, s.repo.Update)
}
