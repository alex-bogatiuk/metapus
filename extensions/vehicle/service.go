package vehicle

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/domain"
	"metapus/internal/platform"
)

// Service provides business logic for Vehicle catalog.
type Service struct {
	*domain.CatalogService[*Vehicle]
	repo      Repository
	numerator platform.Generator
}

// NewService creates a new Vehicle service.
func NewService(
	repo Repository,
	numerator platform.Generator,
) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*Vehicle]{
		Repo:       repo,
		TxManager:  nil,
		Numerator:  numerator,
		EntityName: "vehicle",
	})

	svc := &Service{
		CatalogService: base,
		repo:           repo,
		numerator:      numerator,
	}

	base.Hooks().OnBeforeCreate(svc.prepareForCreate)
	base.Hooks().OnBeforeUpdate(svc.prepareForUpdate)

	return svc
}

func (s *Service) prepareForCreate(ctx context.Context, v *Vehicle) error {
	if v.Code == "" {
		code, err := s.numerator.GetNextNumber(ctx, platform.DefaultNumeratorConfig("VH"), nil, time.Now())
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		v.Code = code
	}

	if v.PlateNumber != "" {
		if exists, _ := s.checkPlateExists(ctx, v.PlateNumber, v.ID); exists {
			return platform.NewConflict("vehicle with this plate number already exists").
				WithDetail("plateNumber", v.PlateNumber)
		}
	}

	return nil
}

func (s *Service) prepareForUpdate(ctx context.Context, v *Vehicle) error {
	if v.PlateNumber != "" {
		if exists, _ := s.checkPlateExists(ctx, v.PlateNumber, v.ID); exists {
			return platform.NewConflict("vehicle with this plate number already exists").
				WithDetail("plateNumber", v.PlateNumber)
		}
	}
	return nil
}

func (s *Service) checkPlateExists(ctx context.Context, plateNumber string, excludeID platform.ID) (bool, error) {
	existing, err := s.repo.FindByPlateNumber(ctx, plateNumber)
	if err != nil {
		return false, nil
	}
	return existing.ID != excludeID, nil
}
