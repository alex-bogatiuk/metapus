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
	_cryptoBalanceMovementsTable = "reg_crypto_balance_movements"
)

// cryptoBalanceMovementColumns defines column order for crypto balance movements.
var _cryptoBalanceMovementColumns = []string{
	"line_id", "recorder_id", "recorder_type", "recorder_version",
	"period", "record_type",
	"wallet_id", "token_id", "amount", "created_at",
}

// cryptoBalanceMovementRowMapper converts a CryptoBalanceMovement to a flat row.
func cryptoBalanceMovementRowMapper(m entity.CryptoBalanceMovement) []any {
	return []any{
		m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion,
		m.Period, m.RecordType,
		m.WalletID, m.TokenID, m.Amount, m.CreatedAt,
	}
}

// CryptoBalanceRepo implements crypto_balance.Repository.
type CryptoBalanceRepo struct {
	BaseAccumulationRepo[entity.CryptoBalanceMovement]
}

// NewCryptoBalanceRepo creates a new crypto balance register repository.
func NewCryptoBalanceRepo() *CryptoBalanceRepo {
	return &CryptoBalanceRepo{
		BaseAccumulationRepo: NewBaseAccumulationRepo[entity.CryptoBalanceMovement](
			_cryptoBalanceMovementsTable,
			_cryptoBalanceMovementColumns,
			cryptoBalanceMovementRowMapper,
		),
	}
}

// GetMovementsByRecorder retrieves movements for a document.
func (r *CryptoBalanceRepo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.CryptoBalanceMovement, error) {
	q := r.Builder().Select(
		"line_id", "recorder_id", "recorder_type", "recorder_version",
		"period", "record_type",
		"wallet_id", "token_id", "amount", "created_at",
	).From(_cryptoBalanceMovementsTable).
		Where(squirrel.Eq{"recorder_id": recorderID}).
		OrderBy("created_at")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var movements []entity.CryptoBalanceMovement
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select crypto balance movements: %w", err)
	}

	return movements, nil
}
