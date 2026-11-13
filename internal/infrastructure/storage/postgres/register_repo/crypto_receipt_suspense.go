package register_repo

import (
	"context"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

var receiptSuspenseColumns = []string{
	"line_id", "recorder_id", "recorder_type", "recorder_version", "period", "record_type",
	"receipt_id", "token_id", "amount", "created_at",
}

type CryptoReceiptSuspenseRepo struct {
	BaseAccumulationRepo[entity.CryptoReceiptSuspenseMovement]
}

func NewCryptoReceiptSuspenseRepo() *CryptoReceiptSuspenseRepo {
	return &CryptoReceiptSuspenseRepo{BaseAccumulationRepo: NewBaseAccumulationRepo(
		"reg_crypto_receipt_suspense_movements", receiptSuspenseColumns,
		func(m entity.CryptoReceiptSuspenseMovement) []any {
			return []any{m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion, m.Period,
				m.RecordType, m.ReceiptID, m.TokenID, m.Amount, m.CreatedAt}
		},
	)}
}

// ListRecorderMovements returns every suspense line of one recorder: the
// receipt itself, a payment, a manual credit, a platform funding or a storno.
func (r *CryptoReceiptSuspenseRepo) ListRecorderMovements(ctx context.Context, recorderID id.ID) ([]entity.CryptoReceiptSuspenseMovement, error) {
	query, args, err := r.Builder().Select(receiptSuspenseColumns...).
		From("reg_crypto_receipt_suspense_movements").
		Where("recorder_id=?", recorderID).OrderBy("created_at,line_id").ToSql()
	if err != nil {
		return nil, fmt.Errorf("build suspense movement query: %w", err)
	}
	var result []entity.CryptoReceiptSuspenseMovement
	if err := pgxscan.Select(ctx, r.GetTxManager(ctx).GetQuerier(ctx), &result, query, args...); err != nil {
		return nil, fmt.Errorf("read suspense movements: %w", err)
	}
	return result, nil
}

func (r *CryptoReceiptSuspenseRepo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]entity.CryptoReceiptSuspenseMovement, error) {
	query, args, err := r.Builder().Select(receiptSuspenseColumns...).
		From("reg_crypto_receipt_suspense_movements").
		Where("receipt_id=? AND recorder_id=?", recorderID, recorderID).OrderBy("created_at,line_id").ToSql()
	if err != nil {
		return nil, fmt.Errorf("build suspense movement query: %w", err)
	}
	var result []entity.CryptoReceiptSuspenseMovement
	if err := pgxscan.Select(ctx, r.GetTxManager(ctx).GetQuerier(ctx), &result, query, args...); err != nil {
		return nil, fmt.Errorf("read suspense movements: %w", err)
	}
	return result, nil
}
