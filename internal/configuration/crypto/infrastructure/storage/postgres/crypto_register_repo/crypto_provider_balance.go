package crypto_register_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/registers/crypto_provider_balance"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/registers"
)

const _cryptoProviderBalanceMovementsTable = "reg_crypto_provider_balance_movements"

var _cryptoProviderBalanceMovementColumns = []string{
	"line_id", "recorder_id", "recorder_type", "recorder_version", "period", "record_type",
	"provider_account_id", "token_id", "operation_type", "external_ref", "amount", "created_at",
}

func cryptoProviderBalanceMovementRowMapper(m crypto_provider_balance.CryptoProviderBalanceMovement) []any {
	return []any{m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion, m.Period, m.RecordType,
		m.ProviderAccountID, m.TokenID, m.OperationType, m.ExternalRef, m.Amount, m.CreatedAt}
}

type CryptoProviderBalanceRepo struct {
	registers.BaseAccumulationRepo[crypto_provider_balance.CryptoProviderBalanceMovement]
}

func NewCryptoProviderBalanceRepo() *CryptoProviderBalanceRepo {
	return &CryptoProviderBalanceRepo{BaseAccumulationRepo: registers.NewBaseAccumulationRepo[crypto_provider_balance.CryptoProviderBalanceMovement](
		_cryptoProviderBalanceMovementsTable, _cryptoProviderBalanceMovementColumns, cryptoProviderBalanceMovementRowMapper,
	)}
}

func (r *CryptoProviderBalanceRepo) AppendReversalsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return appendReversalsByRecorder(ctx, &r.BaseAccumulationRepo, recorderID, beforeVersion, []string{
		"provider_account_id", "token_id", "operation_type", "external_ref",
	})
}

func (r *CryptoProviderBalanceRepo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]crypto_provider_balance.CryptoProviderBalanceMovement, error) {
	q := r.Builder().Select(_cryptoProviderBalanceMovementColumns...).From(_cryptoProviderBalanceMovementsTable).
		Where(squirrel.Eq{"recorder_id": recorderID}).OrderBy("created_at")
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build provider movement query: %w", err)
	}
	var movements []crypto_provider_balance.CryptoProviderBalanceMovement
	if err := pgxscan.Select(ctx, r.GetTxManager(ctx).GetQuerier(ctx), &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select provider movements: %w", err)
	}
	return movements, nil
}
