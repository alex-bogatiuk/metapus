package crypto

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/token"
)

// MerchantTokenConfigRepository defines storage for merchant token config overrides.
type MerchantTokenConfigRepository interface {
	// Get returns the config for a specific merchant+token pair.
	// Returns nil, nil if no override exists.
	Get(ctx context.Context, merchantID, tokenID id.ID) (*MerchantTokenConfig, error)

	// Upsert inserts or updates a merchant token config.
	Upsert(ctx context.Context, cfg *MerchantTokenConfig) error
}

// SweepConfigResolver resolves effective sweep configuration.
// Priority: MerchantTokenConfig (if non-NULL) → Token defaults.
type SweepConfigResolver struct {
	merchantConfigRepo MerchantTokenConfigRepository
	tokenRepo          token.Repository
}

// NewSweepConfigResolver creates a new resolver.
func NewSweepConfigResolver(
	merchantConfigRepo MerchantTokenConfigRepository,
	tokenRepo token.Repository,
) *SweepConfigResolver {
	return &SweepConfigResolver{
		merchantConfigRepo: merchantConfigRepo,
		tokenRepo:          tokenRepo,
	}
}

// Resolve returns the effective sweep config for a merchant+token pair.
// Applies NULL-coalescing: merchant override → token default.
func (r *SweepConfigResolver) Resolve(ctx context.Context, merchantID, tokenID id.ID) (SweepConfig, error) {
	// 1. Get token defaults (always required)
	tok, err := r.tokenRepo.GetByID(ctx, tokenID)
	if err != nil {
		return SweepConfig{}, fmt.Errorf("get token %s: %w", tokenID, err)
	}

	cfg := SweepConfig{
		Threshold:   tok.SweepThreshold,
		MaxAgeHours: tok.SweepMaxAgeHours,
	}

	// 2. Try merchant override (optional)
	if !id.IsNil(merchantID) {
		override, err := r.merchantConfigRepo.Get(ctx, merchantID, tokenID)
		if err != nil {
			// Non-critical: log and use token defaults
			return cfg, nil
		}
		if override != nil {
			// Apply non-NULL overrides
			if override.SweepThreshold != nil {
				cfg.Threshold = *override.SweepThreshold
			}
			if override.SweepMaxAgeHours != nil {
				cfg.MaxAgeHours = *override.SweepMaxAgeHours
			}
		}
	}

	return cfg, nil
}
