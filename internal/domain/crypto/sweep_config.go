package crypto

import (
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// SweepConfig is the effective resolved sweep configuration for a merchant+token pair.
// It is computed by SweepConfigResolver via NULL-coalescing:
// merchant override (reg_merchant_token_config) → token default (cat_tokens).
type SweepConfig struct {
	// Threshold is the minimum accumulated balance to trigger sweep (minor units).
	// Zero means sweep after every payment (legacy behavior).
	Threshold types.CryptoAmount

	// MaxAgeHours is the maximum hours before a forced sweep regardless of threshold.
	// Zero means disabled (only threshold-based sweep).
	MaxAgeHours int
}

// IsZeroThreshold returns true if sweep should happen after every payment.
func (c SweepConfig) IsZeroThreshold() bool {
	return !c.Threshold.IsPositive()
}

// MerchantTokenConfig represents per-merchant sweep overrides.
// NULL fields mean "use token default".
// Stored in reg_merchant_token_config.
type MerchantTokenConfig struct {
	MerchantID       id.ID                `db:"merchant_id" json:"merchantId"`
	TokenID          id.ID                `db:"token_id" json:"tokenId"`
	SweepThreshold   *types.CryptoAmount  `db:"sweep_threshold" json:"sweepThreshold"`
	SweepMaxAgeHours *int                 `db:"sweep_max_age_hours" json:"sweepMaxAgeHours"`
}
