// Package rate_source provides the RateSource catalog.
// RateSources represent exchange rate providers (CoinGecko, Binance, manual entry, etc.).
package rate_source

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// SourceType identifies the provider integration type.
type SourceType string

// RateSource represents an exchange rate provider.
type RateSource struct {
	entity.Catalog

	// SourceType identifies the provider integration (e.g. "coingecko", "binance", "manual").
	SourceType string `db:"source_type" json:"sourceType" meta:"label:Тип источника"`

	// BaseURL is the API base URL for the provider.
	BaseURL *string `db:"base_url" json:"baseUrl,omitempty" meta:"label:API URL"`

	// APIKey is the encrypted API key for the provider.
	APIKey *string `db:"api_key" json:"-"` // never expose in API

	// RateLimitRPM is the maximum number of requests per minute.
	RateLimitRPM int `db:"rate_limit_rpm" json:"rateLimitRpm" meta:"label:Лимит запросов/мин"`

	// Priority determines fallback ordering (lower = higher priority).
	Priority int `db:"priority" json:"priority" meta:"label:Приоритет"`

	// IsActive enables/disables rate fetching from this source.
	IsActive bool `db:"is_active" json:"isActive" meta:"label:Активен"`
}

// NewRateSource creates a new RateSource with required fields.
func NewRateSource(code, name, sourceType string) *RateSource {
	return &RateSource{
		Catalog:      entity.NewCatalog(code, name),
		SourceType:   sourceType,
		RateLimitRPM: 100,
		Priority:     100,
		IsActive:     true,
	}
}

// Validate implements entity.Validatable.
func (rs *RateSource) Validate(ctx context.Context) error {
	if err := rs.Catalog.Validate(ctx); err != nil {
		return err
	}

	if rs.SourceType == "" {
		return apperror.NewValidation("source type is required").
			WithDetail("field", "sourceType")
	}

	if rs.RateLimitRPM < 1 {
		return apperror.NewValidation("rate limit must be at least 1 RPM").
			WithDetail("field", "rateLimitRpm")
	}

	if rs.Priority < 1 {
		return apperror.NewValidation("priority must be at least 1").
			WithDetail("field", "priority")
	}

	return nil
}

// TableName returns the database table name.
func (RateSource) TableName() string { return "cat_rate_sources" }

// ── Repository & Service ──────────────────────────────────────────────────

// Repository defines storage operations for RateSource.
type Repository interface {
	domain.CatalogRepository[*RateSource]
}

// Service provides business logic for RateSource catalog.
type Service struct {
	*domain.CatalogService[*RateSource]
}

// NewService creates a new RateSource service.
func NewService(repo Repository, num numerator.Generator) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*RateSource]{
		Repo:       repo,
		Numerator:  num,
		EntityName: "rate_source",
	})

	return &Service{CatalogService: base}
}
