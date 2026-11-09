package tree_probe

import (
	"context"
	"fmt"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
	"time"
)

type Service struct {
	*domain.CatalogService[*TreeProbe]
}

func NewService(repo Repository, num numerator.Generator) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*TreeProbe]{Definition: &Definition, Repo: repo, Numerator: num})
	base.Hooks().OnBeforeCreate(func(ctx context.Context, e *TreeProbe) error {
		if e.Code != "" {
			return nil
		}
		code, err := num.GetNextNumber(ctx, numerator.DefaultConfig("TR"), nil, time.Now())
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		e.Code = code
		return nil
	})
	return &Service{base}
}
