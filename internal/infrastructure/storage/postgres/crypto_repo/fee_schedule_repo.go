package crypto_repo

import (
	"context"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

// _nilMerchantUUID is used in PK COALESCE for global defaults (merchant_id IS NULL).
const _nilMerchantUUID = "00000000-0000-0000-0000-000000000000"

// FeeScheduleRepo implements crypto.FeeScheduleRepository.
type FeeScheduleRepo struct{}

// NewFeeScheduleRepo creates a new repository.
func NewFeeScheduleRepo() *FeeScheduleRepo {
	return &FeeScheduleRepo{}
}

// Get returns the fee schedule for a specific merchant+token+direction.
// merchantID == nil queries the global default.
// Returns nil, nil if no entry exists.
func (r *FeeScheduleRepo) Get(ctx context.Context, merchantID *id.ID, tokenID id.ID, direction crypto.FeeDirection) (*crypto.FeeSchedule, error) {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)

	var query string
	var args []interface{}

	if merchantID != nil {
		query = `
			SELECT merchant_id, token_id, direction, fixed_fee, percent_bp, min_fee, max_fee, updated_at
			FROM reg_fee_schedule
			WHERE merchant_id = $1 AND token_id = $2 AND direction = $3
		`
		args = []interface{}{*merchantID, tokenID, direction}
	} else {
		query = `
			SELECT merchant_id, token_id, direction, fixed_fee, percent_bp, min_fee, max_fee, updated_at
			FROM reg_fee_schedule
			WHERE merchant_id IS NULL AND token_id = $1 AND direction = $2
		`
		args = []interface{}{tokenID, direction}
	}

	var fs crypto.FeeSchedule
	if err := pgxscan.Get(ctx, querier, &fs, query, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get fee schedule: %w", err)
	}

	return &fs, nil
}

// Upsert inserts or updates a fee schedule entry.
func (r *FeeScheduleRepo) Upsert(ctx context.Context, schedule *crypto.FeeSchedule) error {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)

	const query = `
		INSERT INTO reg_fee_schedule (merchant_id, token_id, direction, fixed_fee, percent_bp, min_fee, max_fee, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (COALESCE(merchant_id, '00000000-0000-0000-0000-000000000000'::UUID), token_id, direction)
		DO UPDATE SET
			fixed_fee  = EXCLUDED.fixed_fee,
			percent_bp = EXCLUDED.percent_bp,
			min_fee    = EXCLUDED.min_fee,
			max_fee    = EXCLUDED.max_fee,
			updated_at = now()
	`

	_, err := querier.Exec(ctx, query,
		schedule.MerchantID,
		schedule.TokenID,
		schedule.Direction,
		schedule.FixedFee,
		schedule.PercentBP,
		schedule.MinFee,
		schedule.MaxFee,
	)
	if err != nil {
		return fmt.Errorf("upsert fee schedule: %w", err)
	}

	return nil
}

// ListByMerchant returns all fee schedules for a merchant.
func (r *FeeScheduleRepo) ListByMerchant(ctx context.Context, merchantID id.ID) ([]crypto.FeeSchedule, error) {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)

	const query = `
		SELECT merchant_id, token_id, direction, fixed_fee, percent_bp, min_fee, max_fee, updated_at
		FROM reg_fee_schedule
		WHERE merchant_id = $1
		ORDER BY token_id, direction
	`

	var schedules []crypto.FeeSchedule
	if err := pgxscan.Select(ctx, querier, &schedules, query, merchantID); err != nil {
		return nil, fmt.Errorf("list fee schedules by merchant: %w", err)
	}

	return schedules, nil
}

// ListGlobal returns all global default fee schedules (merchant_id IS NULL).
func (r *FeeScheduleRepo) ListGlobal(ctx context.Context) ([]crypto.FeeSchedule, error) {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)

	const query = `
		SELECT merchant_id, token_id, direction, fixed_fee, percent_bp, min_fee, max_fee, updated_at
		FROM reg_fee_schedule
		WHERE merchant_id IS NULL
		ORDER BY token_id, direction
	`

	var schedules []crypto.FeeSchedule
	if err := pgxscan.Select(ctx, querier, &schedules, query); err != nil {
		return nil, fmt.Errorf("list global fee schedules: %w", err)
	}

	return schedules, nil
}

// Delete removes a fee schedule entry.
// merchantID == nil deletes the global default.
func (r *FeeScheduleRepo) Delete(ctx context.Context, merchantID *id.ID, tokenID id.ID, direction crypto.FeeDirection) error {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)

	var query string
	var args []interface{}

	if merchantID != nil {
		query = `DELETE FROM reg_fee_schedule WHERE merchant_id = $1 AND token_id = $2 AND direction = $3`
		args = []interface{}{*merchantID, tokenID, direction}
	} else {
		query = `DELETE FROM reg_fee_schedule WHERE merchant_id IS NULL AND token_id = $1 AND direction = $2`
		args = []interface{}{tokenID, direction}
	}

	tag, err := querier.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete fee schedule: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("fee schedule not found")
	}

	return nil
}

// compile-time check
var _ crypto.FeeScheduleRepository = (*FeeScheduleRepo)(nil)
