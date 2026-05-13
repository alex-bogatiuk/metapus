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
	_cryptoMerchantBalanceMovementsTable = "reg_crypto_merchant_balance_movements"
)

// _cryptoMerchantBalanceMovementColumns defines column order for merchant balance movements.
var _cryptoMerchantBalanceMovementColumns = []string{
	"line_id", "recorder_id", "recorder_type", "recorder_version",
	"period", "record_type",
	"merchant_id", "token_id", "amount", "created_at",
}

// cryptoMerchantBalanceMovementRowMapper converts a CryptoMerchantBalanceMovement to a flat row.
func cryptoMerchantBalanceMovementRowMapper(m entity.CryptoMerchantBalanceMovement) []any {
	return []any{
		m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion,
		m.Period, m.RecordType,
		m.MerchantID, m.TokenID, m.Amount, m.CreatedAt,
	}
}

// CryptoMerchantBalanceRepo implements crypto_merchant_balance.Repository.
type CryptoMerchantBalanceRepo struct {
	BaseAccumulationRepo[entity.CryptoMerchantBalanceMovement]
}

// NewCryptoMerchantBalanceRepo creates a new crypto merchant balance register repository.
func NewCryptoMerchantBalanceRepo() *CryptoMerchantBalanceRepo {
	return &CryptoMerchantBalanceRepo{
		BaseAccumulationRepo: NewBaseAccumulationRepo[entity.CryptoMerchantBalanceMovement](
			_cryptoMerchantBalanceMovementsTable,
			_cryptoMerchantBalanceMovementColumns,
			cryptoMerchantBalanceMovementRowMapper,
		),
	}
}

// GetMovementsByRecorder retrieves movements for a document.
func (r *CryptoMerchantBalanceRepo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.CryptoMerchantBalanceMovement, error) {
	q := r.Builder().Select(
		"line_id", "recorder_id", "recorder_type", "recorder_version",
		"period", "record_type",
		"merchant_id", "token_id", "amount", "created_at",
	).From(_cryptoMerchantBalanceMovementsTable).
		Where(squirrel.Eq{"recorder_id": recorderID}).
		OrderBy("created_at")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var movements []entity.CryptoMerchantBalanceMovement
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select crypto merchant balance movements: %w", err)
	}

	return movements, nil
}
