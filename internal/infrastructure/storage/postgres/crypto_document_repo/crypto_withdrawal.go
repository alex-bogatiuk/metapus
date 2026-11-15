package crypto_document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
)

// CryptoWithdrawalRepo implements crypto_withdrawal.Repository.
type CryptoWithdrawalRepo struct {
	*document_repo.BaseDocumentRepo[*crypto_withdrawal.CryptoWithdrawal]
}

// NewCryptoWithdrawalRepo creates a new crypto withdrawal repository.
func NewCryptoWithdrawalRepo() *CryptoWithdrawalRepo {
	repo := &CryptoWithdrawalRepo{
		BaseDocumentRepo: document_repo.NewDocumentRepoForDefinition[*crypto_withdrawal.CryptoWithdrawal](crypto_withdrawal.Definition,
			postgres.ExtractDBColumns[crypto_withdrawal.CryptoWithdrawal](),
			func() *crypto_withdrawal.CryptoWithdrawal { return &crypto_withdrawal.CryptoWithdrawal{} },
		),
	}
	repo.RegisterRLSDimension("merchant", "merchant_id")
	return repo
}

// FindPending returns withdrawals in Created status for processing.
func (r *CryptoWithdrawalRepo) FindPending(ctx context.Context, limit int) ([]*crypto_withdrawal.CryptoWithdrawal, error) {
	return r.ListByStatus(ctx, crypto_withdrawal.WithdrawalStatusCreated, limit)
}

// ListByStatus returns withdrawals with the specified lifecycle status.
func (r *CryptoWithdrawalRepo) ListByStatus(ctx context.Context, status crypto_withdrawal.WithdrawalStatus, limit int) ([]*crypto_withdrawal.CryptoWithdrawal, error) {
	q := r.Builder().Select("*").
		From(crypto_withdrawal.Definition.Table).
		Where(squirrel.Eq{"status": status}).
		Where(squirrel.Eq{"deletion_mark": false}).
		OrderBy("updated_at ASC").
		Limit(uint64(limit))

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var withdrawals []*crypto_withdrawal.CryptoWithdrawal
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &withdrawals, sql, args...); err != nil {
		return nil, fmt.Errorf("list withdrawals by status: %w", err)
	}
	return withdrawals, nil
}
