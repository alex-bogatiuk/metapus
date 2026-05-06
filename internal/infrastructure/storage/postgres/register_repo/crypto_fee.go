package register_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

const (
	_cryptoFeeMovementsTable = "reg_crypto_fee_movements"
)

// _cryptoFeeMovementColumns defines column order for crypto fee movements.
var _cryptoFeeMovementColumns = []string{
	"line_id", "recorder_id", "recorder_type", "recorder_version",
	"period", "record_type",
	"merchant_id", "token_id", "fee_type", "amount", "created_at",
}

// cryptoFeeMovementRowMapper converts a CryptoFeeMovement to a flat row.
func cryptoFeeMovementRowMapper(m entity.CryptoFeeMovement) []any {
	return []any{
		m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion,
		m.Period, m.RecordType,
		m.MerchantID, m.TokenID, m.FeeType, m.Amount, m.CreatedAt,
	}
}

// CryptoFeeRepo implements crypto_fee.Repository.
type CryptoFeeRepo struct {
	BaseAccumulationRepo[entity.CryptoFeeMovement]
}

// NewCryptoFeeRepo creates a new crypto fee register repository.
func NewCryptoFeeRepo() *CryptoFeeRepo {
	return &CryptoFeeRepo{
		BaseAccumulationRepo: NewBaseAccumulationRepo[entity.CryptoFeeMovement](
			_cryptoFeeMovementsTable,
			_cryptoFeeMovementColumns,
			cryptoFeeMovementRowMapper,
		),
	}
}

// GetMovementsByRecorder retrieves movements for a document.
func (r *CryptoFeeRepo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.CryptoFeeMovement, error) {
	q := r.Builder().Select(
		"line_id", "recorder_id", "recorder_type", "recorder_version",
		"period", "record_type",
		"merchant_id", "token_id", "fee_type", "amount", "created_at",
	).From(_cryptoFeeMovementsTable).
		Where(squirrel.Eq{"recorder_id": recorderID}).
		OrderBy("created_at")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var movements []entity.CryptoFeeMovement
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select crypto fee movements: %w", err)
	}

	return movements, nil
}
