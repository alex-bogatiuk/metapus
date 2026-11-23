package crypto_repo

import (
	"context"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

// MerchantTokenConfigRepo implements crypto.MerchantTokenConfigRepository.
type MerchantTokenConfigRepo struct{}

// NewMerchantTokenConfigRepo creates a new repository.
func NewMerchantTokenConfigRepo() *MerchantTokenConfigRepo {
	return &MerchantTokenConfigRepo{}
}

// Get returns the config for a specific merchant+token pair.
// Returns nil, nil if no override exists.
func (r *MerchantTokenConfigRepo) Get(ctx context.Context, merchantID, tokenID id.ID) (*crypto.MerchantTokenConfig, error) {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)

	const query = `
		SELECT merchant_id, token_id, sweep_threshold, sweep_max_age_hours, sweep_retain_amount
		FROM reg_merchant_token_config
		WHERE merchant_id = $1 AND token_id = $2
	`

	var cfg crypto.MerchantTokenConfig
	if err := pgxscan.Get(ctx, querier, &cfg, query, merchantID, tokenID); err != nil {
		if pgxscan.NotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get merchant token config (%s, %s): %w", merchantID, tokenID, err)
	}

	return &cfg, nil
}

// Upsert inserts or updates a merchant token config.
func (r *MerchantTokenConfigRepo) Upsert(ctx context.Context, cfg *crypto.MerchantTokenConfig) error {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)

	const query = `
		INSERT INTO reg_merchant_token_config (merchant_id, token_id, sweep_threshold, sweep_max_age_hours, sweep_retain_amount)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (merchant_id, token_id)
		DO UPDATE SET
			sweep_threshold = EXCLUDED.sweep_threshold,
			sweep_max_age_hours = EXCLUDED.sweep_max_age_hours,
			sweep_retain_amount = EXCLUDED.sweep_retain_amount,
			updated_at = now()
	`

	_, err := querier.Exec(ctx, query,
		cfg.MerchantID, cfg.TokenID, cfg.SweepThreshold, cfg.SweepMaxAgeHours, cfg.SweepRetainAmount,
	)
	if err != nil {
		return fmt.Errorf("upsert merchant token config: %w", err)
	}

	return nil
}
