package crypto_register_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

const _cryptoPlatformCostMovementsTable = "reg_crypto_platform_cost_movements"

var _cryptoPlatformCostMovementColumns = []string{
	"line_id", "recorder_id", "recorder_type", "recorder_version",
	"period", "record_type",
	"network_id", "operation_token_id", "cost_token_id",
	"cost_type", "purpose", "provider", "external_order_id", "amount", "created_at",
}

func cryptoPlatformCostMovementRowMapper(m crypto_platform_cost.CryptoPlatformCostMovement) []any {
	return []any{
		m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion,
		m.Period, m.RecordType,
		m.NetworkID, m.OperationTokenID, m.CostTokenID,
		m.CostType, m.Purpose, m.Provider, m.ExternalOrderID, m.Amount, m.CreatedAt,
	}
}

// CryptoPlatformCostRepo implements crypto_platform_cost.Repository.
type CryptoPlatformCostRepo struct {
	register_repo.BaseAccumulationRepo[crypto_platform_cost.CryptoPlatformCostMovement]
}

// NewCryptoPlatformCostRepo creates a new crypto platform cost register repository.
func NewCryptoPlatformCostRepo() *CryptoPlatformCostRepo {
	return &CryptoPlatformCostRepo{
		BaseAccumulationRepo: register_repo.NewBaseAccumulationRepo[crypto_platform_cost.CryptoPlatformCostMovement](
			_cryptoPlatformCostMovementsTable,
			_cryptoPlatformCostMovementColumns,
			cryptoPlatformCostMovementRowMapper,
		),
	}
}

func (r *CryptoPlatformCostRepo) AppendReversalsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return appendReversalsByRecorder(ctx, &r.BaseAccumulationRepo, recorderID, beforeVersion, []string{
		"network_id", "operation_token_id", "cost_token_id", "cost_type", "purpose", "provider", "external_order_id",
	})
}

// GetMovementsByRecorder retrieves platform cost movements for a document.
func (r *CryptoPlatformCostRepo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]crypto_platform_cost.CryptoPlatformCostMovement, error) {
	q := r.Builder().Select(
		"line_id", "recorder_id", "recorder_type", "recorder_version",
		"period", "record_type",
		"network_id", "operation_token_id", "cost_token_id",
		"cost_type", "purpose", "provider", "external_order_id", "amount", "created_at",
	).From(_cryptoPlatformCostMovementsTable).
		Where(squirrel.Eq{"recorder_id": recorderID}).
		OrderBy("created_at")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var movements []crypto_platform_cost.CryptoPlatformCostMovement
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select crypto platform cost movements: %w", err)
	}
	return movements, nil
}
