package register_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/registers/crypto_merchant_balance"
)

const (
	_cryptoMerchantBalanceMovementsTable = "reg_crypto_merchant_balance_movements"
	_cryptoMerchantBalanceBalancesTable  = "reg_crypto_merchant_balance_balances"
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

// GetBalancesForUpdate returns balances for merchant+token pairs with pessimistic locking
// in deterministic key order (deadlock-safe). Keys not found are returned with Amount=0.
// Analogous to StockRepo.GetBalancesForUpdate.
func (r *CryptoMerchantBalanceRepo) GetBalancesForUpdate(
	ctx context.Context, keys []crypto_merchant_balance.MerchantBalanceKey,
) ([]crypto_merchant_balance.MerchantBalanceEntry, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	// Sort keys for resource ordering (prevents deadlocks).
	sortedKeys := make([]crypto_merchant_balance.MerchantBalanceKey, len(keys))
	copy(sortedKeys, keys)
	crypto_merchant_balance.SortMerchantBalanceKeys(sortedKeys)

	const lockSQL = `
		SELECT merchant_id, token_id, amount
		FROM reg_crypto_merchant_balance_balances
		WHERE merchant_id = $1 AND token_id = $2
		FOR UPDATE
	`

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	b := &pgx.Batch{}
	for _, k := range sortedKeys {
		b.Queue(lockSQL, k.MerchantID, k.TokenID)
	}

	br := querier.SendBatch(ctx, b)
	defer func() {
		_ = br.Close()
	}()

	type keyStr = string
	loaded := make(map[keyStr]crypto_merchant_balance.MerchantBalanceEntry, len(sortedKeys))
	for _, k := range sortedKeys {
		var entry crypto_merchant_balance.MerchantBalanceEntry
		rows, err := br.Query()
		if err != nil {
			return nil, fmt.Errorf("batch query error: %w", err)
		}

		if rows.Next() {
			if err := pgxscan.ScanRow(&entry, rows); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan merchant balance: %w", err)
			}
			loaded[k.MerchantID.String()+"-"+k.TokenID.String()] = entry
		}
		rows.Close()
	}

	// Return in original key order, filling missing entries with zero.
	result := make([]crypto_merchant_balance.MerchantBalanceEntry, len(keys))
	for i, k := range keys {
		ks := k.MerchantID.String() + "-" + k.TokenID.String()
		if entry, ok := loaded[ks]; ok {
			result[i] = entry
		} else {
			result[i] = crypto_merchant_balance.MerchantBalanceEntry{
				MerchantID: k.MerchantID,
				TokenID:    k.TokenID,
				Amount:     0,
			}
		}
	}

	return result, nil
}

