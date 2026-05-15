package crypto

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
)

// FeeScheduleRepository defines storage operations for fee schedule entries.
type FeeScheduleRepository interface {
	// Get returns the fee schedule for a specific merchant+token+direction.
	// merchantID == nil queries the global default.
	// Returns nil, nil if no entry exists.
	Get(ctx context.Context, merchantID *id.ID, tokenID id.ID, direction FeeDirection) (*FeeSchedule, error)

	// Upsert inserts or updates a fee schedule entry.
	Upsert(ctx context.Context, schedule *FeeSchedule) error

	// ListByMerchant returns all fee schedules for a merchant.
	ListByMerchant(ctx context.Context, merchantID id.ID) ([]FeeSchedule, error)

	// ListGlobal returns all global default fee schedules (merchant_id IS NULL).
	ListGlobal(ctx context.Context) ([]FeeSchedule, error)

	// Delete removes a fee schedule entry.
	// merchantID == nil deletes the global default.
	Delete(ctx context.Context, merchantID *id.ID, tokenID id.ID, direction FeeDirection) error
}

// FeeConfigResolver resolves effective fee configuration.
// Priority: merchant-specific → global default → zero fee.
type FeeConfigResolver struct {
	repo FeeScheduleRepository
}

// NewFeeConfigResolver creates a new resolver.
func NewFeeConfigResolver(repo FeeScheduleRepository) *FeeConfigResolver {
	return &FeeConfigResolver{repo: repo}
}

// Resolve returns the effective fee for a merchant+token+direction.
// Applies NULL-coalescing:
//  1. Merchant-specific entry (merchant_id = :m, token_id = :t, direction = :d)
//  2. Global default entry  (merchant_id IS NULL, token_id = :t, direction = :d)
//  3. Zero fee (no entries found)
func (r *FeeConfigResolver) Resolve(ctx context.Context, merchantID, tokenID id.ID, direction FeeDirection) (EffectiveFee, error) {
	// 1. Try merchant-specific
	if !id.IsNil(merchantID) {
		schedule, err := r.repo.Get(ctx, &merchantID, tokenID, direction)
		if err != nil {
			// Non-critical: log and fall through to global default
		} else if schedule != nil {
			return toEffectiveFee(schedule), nil
		}
	}

	// 2. Try global default
	schedule, err := r.repo.Get(ctx, nil, tokenID, direction)
	if err != nil {
		return EffectiveFee{}, fmt.Errorf("get global fee schedule (token=%s, dir=%s): %w", tokenID, direction, err)
	}
	if schedule != nil {
		return toEffectiveFee(schedule), nil
	}

	// 3. No entry found → zero fee
	return EffectiveFee{}, nil
}

// toEffectiveFee converts a FeeSchedule to EffectiveFee.
func toEffectiveFee(s *FeeSchedule) EffectiveFee {
	return EffectiveFee{
		FixedFee:  s.FixedFee,
		PercentBP: s.PercentBP,
		MinFee:    s.MinFee,
		MaxFee:    s.MaxFee,
	}
}
