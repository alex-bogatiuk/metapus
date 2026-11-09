// Package rate_source provides the RateSource catalog.
// RateSources represent exchange rate providers (CoinGecko, Binance, manual entry, etc.).
package rate_source

import (
	"context"
	"strings"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// SourceType identifies the provider integration type.
type SourceType string

// RateSource represents an exchange rate provider.
type RateSource struct {
	entity.Catalog

	// SourceType identifies the provider integration (e.g. "coingecko", "binance", "manual").
	SourceType SourceType `db:"source_type" json:"sourceType" meta:"label:Тип источника"`

	// BaseURL is the API base URL for the provider.
	BaseURL *string `db:"base_url" json:"baseUrl,omitempty" meta:"label:API URL"`

	IntegrationPoolID *id.ID `db:"integration_pool_id" json:"integrationPoolId,omitempty" meta:"label:Пул аккаунтов,ref:integration_pool"`

	// APIKey is a provider credential stored as a catalog requisite. The stored
	// value is never serialized; metadata exposes only a write-only form field.
	APIKey *string `db:"api_key" json:"-" meta:"name:apiKey,label:Ключ API,writeOnly:true,list:false"`

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
		SourceType:   SourceType(sourceType),
		RateLimitRPM: 30,
		Priority:     100,
		IsActive:     true,
	}
}

// SetAPIKey normalizes an explicitly supplied credential. An empty value clears
// the requisite; omission is handled by the DTO and leaves the current value.
func (rs *RateSource) SetAPIKey(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		rs.APIKey = nil
		return
	}
	rs.APIKey = &value
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

	if rs.APIKey != nil {
		if len(*rs.APIKey) > 512 || strings.ContainsAny(*rs.APIKey, "\r\n") {
			return apperror.NewValidation("API key must be at most 512 bytes and contain no line breaks").
				WithDetail("field", "apiKey")
		}
	}

	return nil
}

// TableName returns the database table name.
func (RateSource) TableName() string { return "cat_rate_sources" }

// ── Repository & Service ──────────────────────────────────────────────────

// Repository defines storage operations for RateSource.
type Repository interface {
	domain.CatalogRepository[*RateSource]
	IsUsed(context.Context, id.ID) (bool, error)
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
		Definition: &Definition,
	})

	base.Hooks().OnBeforeUpdate(func(ctx context.Context, updated *RateSource) error {
		previous, err := repo.GetByID(ctx, updated.ID)
		if err != nil {
			return err
		}
		if previous.SourceType != updated.SourceType {
			used, err := repo.IsUsed(ctx, updated.ID)
			if err != nil {
				return err
			}
			if used {
				return apperror.NewBusinessRule("RATE_SOURCE_TYPE_LOCKED", "Тип используемого источника изменить нельзя")
			}
		}
		return nil
	})
	return &Service{CatalogService: base}
}
