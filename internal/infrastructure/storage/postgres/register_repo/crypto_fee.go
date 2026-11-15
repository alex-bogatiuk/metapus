package register_repo

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/registers/crypto_fee"
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
func cryptoFeeMovementRowMapper(m crypto_fee.CryptoFeeMovement) []any {
	return []any{
		m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion,
		m.Period, m.RecordType,
		m.MerchantID, m.TokenID, m.FeeType, m.Amount, m.CreatedAt,
	}
}

// CryptoFeeRepo implements crypto_fee.Repository.
type CryptoFeeRepo struct {
	BaseAccumulationRepo[crypto_fee.CryptoFeeMovement]
}

// NewCryptoFeeRepo creates a new crypto fee register repository.
func NewCryptoFeeRepo() *CryptoFeeRepo {
	return &CryptoFeeRepo{
		BaseAccumulationRepo: NewBaseAccumulationRepo[crypto_fee.CryptoFeeMovement](
			_cryptoFeeMovementsTable,
			_cryptoFeeMovementColumns,
			cryptoFeeMovementRowMapper,
		),
	}
}

func (r *CryptoFeeRepo) AppendReversalsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.appendReversalsByRecorder(ctx, recorderID, beforeVersion, []string{"merchant_id", "token_id", "fee_type"})
}

// GetMovementsByRecorder retrieves movements for a document.
func (r *CryptoFeeRepo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]crypto_fee.CryptoFeeMovement, error) {
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

	var movements []crypto_fee.CryptoFeeMovement
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select crypto fee movements: %w", err)
	}

	return movements, nil
}

// GetBalancesForUpdate locks fee balance rows in deterministic order.
func (r *CryptoFeeRepo) GetBalancesForUpdate(ctx context.Context, keys []crypto_fee.BalanceKey) ([]crypto_fee.BalanceEntry, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	sort.Slice(keys, func(i, j int) bool {
		if c := bytes.Compare(keys[i].MerchantID[:], keys[j].MerchantID[:]); c != 0 {
			return c < 0
		}
		if c := bytes.Compare(keys[i].TokenID[:], keys[j].TokenID[:]); c != 0 {
			return c < 0
		}
		return keys[i].FeeType < keys[j].FeeType
	})

	conditions := make(squirrel.Or, 0, len(keys))
	for _, key := range keys {
		conditions = append(conditions, squirrel.And{
			squirrel.Eq{"merchant_id": key.MerchantID},
			squirrel.Eq{"token_id": key.TokenID},
			squirrel.Eq{"fee_type": key.FeeType},
		})
	}

	q := r.Builder().
		Select("merchant_id", "token_id", "fee_type", "balance").
		From("reg_crypto_fee_balances").
		Where(conditions).
		OrderBy("merchant_id", "token_id", "fee_type").
		Suffix("FOR UPDATE")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build fee balances for update: %w", err)
	}

	var balances []crypto_fee.BalanceEntry
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &balances, sql, args...); err != nil {
		return nil, fmt.Errorf("select fee balances for update: %w", err)
	}
	return balances, nil
}
