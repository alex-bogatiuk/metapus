package crypto_document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/treasury_withdrawal"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/document_repo"
)

// TreasuryWithdrawalRepo implements treasury_withdrawal.Repository.
type TreasuryWithdrawalRepo struct {
	*document_repo.BaseDocumentRepo[*treasury_withdrawal.TreasuryWithdrawal]
}

// NewTreasuryWithdrawalRepo creates a treasury withdrawal repository.
func NewTreasuryWithdrawalRepo() *TreasuryWithdrawalRepo {
	return &TreasuryWithdrawalRepo{
		BaseDocumentRepo: document_repo.NewDocumentRepoForDefinition[*treasury_withdrawal.TreasuryWithdrawal](treasury_withdrawal.Definition,
			postgres.ExtractDBColumns[treasury_withdrawal.TreasuryWithdrawal](),
			func() *treasury_withdrawal.TreasuryWithdrawal { return &treasury_withdrawal.TreasuryWithdrawal{} },
		),
	}
}

// ListByStatus returns treasury withdrawals with the specified lifecycle status.
func (r *TreasuryWithdrawalRepo) ListByStatus(ctx context.Context, status treasury_withdrawal.Status, limit int) ([]*treasury_withdrawal.TreasuryWithdrawal, error) {
	q := r.Builder().Select("*").
		From(treasury_withdrawal.Definition.Table).
		Where(squirrel.Eq{"status": status}).
		Where(squirrel.Eq{"deletion_mark": false}).
		OrderBy("updated_at ASC").
		Limit(uint64(limit))

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var docs []*treasury_withdrawal.TreasuryWithdrawal
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &docs, sql, args...); err != nil {
		return nil, fmt.Errorf("list treasury withdrawals by status: %w", err)
	}
	return docs, nil
}
