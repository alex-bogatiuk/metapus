package crypto

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/tariff"
)

// FeeDirection represents the type of operation for fee calculation.
type FeeDirection string

const (
	FeeDirectionProcessing FeeDirection = "processing" // incoming payment from merchant's customer
	FeeDirectionWithdrawal FeeDirection = "withdrawal" // merchant withdraws funds to own wallet
	FeeDirectionPayout     FeeDirection = "payout"     // platform pays merchant's customer on request
	FeeDirectionSettlement FeeDirection = "settlement" // periodic platform→merchant settlement
	FeeDirectionRefund     FeeDirection = "refund"     // refund to customer (reverse of processing)
)

// _validFeeDirections is the set of valid FeeDirection values (compile-time constant).
var _validFeeDirections = map[FeeDirection]bool{
	FeeDirectionProcessing: true,
	FeeDirectionWithdrawal: true,
	FeeDirectionPayout:     true,
	FeeDirectionSettlement: true,
	FeeDirectionRefund:     true,
}

// IsValidFeeDirection checks if a direction string is valid.
func IsValidFeeDirection(d FeeDirection) bool {
	return _validFeeDirections[d]
}

// FeeSchedule represents a fee configuration entry (one row in reg_fee_schedule).
// MerchantID == nil means this is a global default for all merchants.
type FeeSchedule struct {
	MerchantID *id.ID             `db:"merchant_id" json:"merchantId"`
	TokenID    id.ID              `db:"token_id" json:"tokenId"`
	Direction  FeeDirection       `db:"direction" json:"direction"`
	FixedFee   types.CryptoAmount `db:"fixed_fee" json:"fixedFee"`
	PercentBP  int                `db:"percent_bp" json:"percentBp"`
	MinFee     types.CryptoAmount `db:"min_fee" json:"minFee"`
	MaxFee     types.CryptoAmount `db:"max_fee" json:"maxFee"`
	UpdatedAt  time.Time          `db:"updated_at" json:"updatedAt"`
}

// EffectiveFee is the resolved fee config for a specific operation.
// Computed by FeeConfigResolver via NULL-coalescing:
// merchant-specific → global default → zero fee.
type EffectiveFee struct {
	FixedFee  types.CryptoAmount // fixed part (token minor units)
	PercentBP int                // percentage in basis points [0..10000]
	MinFee    types.CryptoAmount // minimum total fee (0 = no floor)
	MaxFee    types.CryptoAmount // maximum total fee (0 = no cap)
}

// Calculate computes the actual fee amount for a given operation amount with
// the shared processing tariff rule.
func (f EffectiveFee) Calculate(amount types.CryptoAmount) types.CryptoAmount {
	return f.Tariff().Amount(amount)
}

// Tariff is the resolved fee as the processing tariff a document snapshots.
func (f EffectiveFee) Tariff() tariff.Fee {
	return tariff.Fee{Fixed: f.FixedFee, PercentBP: f.PercentBP, Min: f.MinFee, Max: f.MaxFee}
}

// IsZero returns true if this fee config would always produce zero fee.
func (f EffectiveFee) IsZero() bool {
	return !f.FixedFee.IsPositive() && f.PercentBP <= 0 && !f.MinFee.IsPositive()
}
