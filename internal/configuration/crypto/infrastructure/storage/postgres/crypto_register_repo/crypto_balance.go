package crypto_register_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/domain/registers/crypto_balance"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

const (
	_cryptoBalanceMovementsTable = "reg_crypto_balance_movements"
	_cryptoBalanceBalancesTable  = "reg_crypto_balance_balances"
)

// cryptoBalanceMovementColumns defines column order for crypto balance movements.
var _cryptoBalanceMovementColumns = []string{
	"line_id", "recorder_id", "recorder_type", "recorder_version",
	"period", "record_type",
	"wallet_id", "token_id", "amount", "created_at",
}

// cryptoBalanceMovementRowMapper converts a CryptoBalanceMovement to a flat row.
func cryptoBalanceMovementRowMapper(m crypto_balance.CryptoBalanceMovement) []any {
	return []any{
		m.LineID, m.RecorderID, m.RecorderType, m.RecorderVersion,
		m.Period, m.RecordType,
		m.WalletID, m.TokenID, m.Amount, m.CreatedAt,
	}
}

// CryptoBalanceRepo implements crypto_balance.Repository.
type CryptoBalanceRepo struct {
	register_repo.BaseAccumulationRepo[crypto_balance.CryptoBalanceMovement]
}

// NewCryptoBalanceRepo creates a new crypto balance register repository.
func NewCryptoBalanceRepo() *CryptoBalanceRepo {
	return &CryptoBalanceRepo{
		BaseAccumulationRepo: register_repo.NewBaseAccumulationRepo[crypto_balance.CryptoBalanceMovement](
			_cryptoBalanceMovementsTable,
			_cryptoBalanceMovementColumns,
			cryptoBalanceMovementRowMapper,
		),
	}
}

func (r *CryptoBalanceRepo) LockPostingResources(ctx context.Context, recorderID id.ID, movements []crypto_balance.CryptoBalanceMovement) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("crypto posting resource locks require a transaction")
	}
	wallets := make([]id.ID, 0, len(movements))
	for _, movement := range movements {
		wallets = append(wallets, movement.WalletID)
	}
	_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT fn_crypto_lock_custody_wallets(ARRAY(
		SELECT unnest($2::uuid[]) UNION SELECT wallet_id FROM reg_crypto_balance_movements WHERE recorder_id=$1))`, recorderID, wallets)
	return err
}

var _ crypto_balance.ResourceLocker = (*CryptoBalanceRepo)(nil)

func (r *CryptoBalanceRepo) AppendReversalsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return appendReversalsByRecorder(ctx, &r.BaseAccumulationRepo, recorderID, beforeVersion, []string{"wallet_id", "token_id"})
}

// GetMovementsByRecorder retrieves movements for a document.
func (r *CryptoBalanceRepo) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]crypto_balance.CryptoBalanceMovement, error) {
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

	var movements []crypto_balance.CryptoBalanceMovement
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &movements, sql, args...); err != nil {
		return nil, fmt.Errorf("select crypto balance movements: %w", err)
	}

	return movements, nil
}

// GetBalancesForUpdate returns balances for wallet+token pairs with pessimistic locking
// in deterministic key order. Keys not found are returned with Amount=0.
func (r *CryptoBalanceRepo) GetBalancesForUpdate(
	ctx context.Context, keys []crypto_balance.BalanceKey,
) ([]crypto_balance.BalanceEntry, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	sortedKeys := make([]crypto_balance.BalanceKey, len(keys))
	copy(sortedKeys, keys)
	crypto_balance.SortBalanceKeys(sortedKeys)

	const lockSQL = `
		SELECT wallet_id, token_id, amount
		FROM reg_crypto_balance_balances
		WHERE wallet_id = $1 AND token_id = $2
		FOR UPDATE
	`

	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	b := &pgx.Batch{}
	for _, k := range sortedKeys {
		b.Queue(lockSQL, k.WalletID, k.TokenID)
	}

	br := querier.SendBatch(ctx, b)
	defer func() {
		_ = br.Close()
	}()

	type keyStr = string
	loaded := make(map[keyStr]crypto_balance.BalanceEntry, len(sortedKeys))
	for _, k := range sortedKeys {
		var entry crypto_balance.BalanceEntry
		rows, err := br.Query()
		if err != nil {
			return nil, fmt.Errorf("batch query error: %w", err)
		}

		if rows.Next() {
			if err := pgxscan.ScanRow(&entry, rows); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan crypto balance: %w", err)
			}
			loaded[k.WalletID.String()+"-"+k.TokenID.String()] = entry
		}
		rows.Close()
	}

	result := make([]crypto_balance.BalanceEntry, len(keys))
	for i, k := range keys {
		ks := k.WalletID.String() + "-" + k.TokenID.String()
		if entry, ok := loaded[ks]; ok {
			result[i] = entry
		} else {
			result[i] = crypto_balance.BalanceEntry{
				WalletID: k.WalletID,
				TokenID:  k.TokenID,
				Amount:   types.ZeroRawAmount(),
			}
		}
	}

	return result, nil
}
