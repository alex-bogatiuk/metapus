package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/infrastructure/storage/postgres"
)

const _cryptoWithdrawalsTable = "doc_crypto_withdrawals"

// CryptoWithdrawalRepo implements crypto_withdrawal.Repository.
type CryptoWithdrawalRepo struct {
	*BaseDocumentRepo[*crypto_withdrawal.CryptoWithdrawal]
}

// NewCryptoWithdrawalRepo creates a new crypto withdrawal repository.
func NewCryptoWithdrawalRepo() *CryptoWithdrawalRepo {
	repo := &CryptoWithdrawalRepo{
		BaseDocumentRepo: NewBaseDocumentRepo[*crypto_withdrawal.CryptoWithdrawal](
			_cryptoWithdrawalsTable,
			postgres.ExtractDBColumns[crypto_withdrawal.CryptoWithdrawal](),
			func() *crypto_withdrawal.CryptoWithdrawal { return &crypto_withdrawal.CryptoWithdrawal{} },
		),
	}
	repo.RegisterRLSDimension("merchant", "merchant_id")
	return repo
}

// FindPending returns withdrawals in Created status for processing.
func (r *CryptoWithdrawalRepo) FindPending(ctx context.Context, limit int) ([]*crypto_withdrawal.CryptoWithdrawal, error) {
	q := r.Builder().Select("*").
		From(_cryptoWithdrawalsTable).
		Where(squirrel.Eq{"status": crypto_withdrawal.WithdrawalStatusCreated}).
		Where(squirrel.Eq{"deletion_mark": false}).
		OrderBy("created_at ASC").
		Limit(uint64(limit))

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var withdrawals []*crypto_withdrawal.CryptoWithdrawal
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &withdrawals, sql, args...); err != nil {
		return nil, fmt.Errorf("find pending: %w", err)
	}
	return withdrawals, nil
}
