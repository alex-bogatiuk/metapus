package catalog_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/infrastructure/storage/postgres"
)

const _tokenTable = "cat_tokens"

// TokenRepo implements token.Repository.
type TokenRepo struct {
	*BaseCatalogRepo[*token.Token]
}

// NewTokenRepo creates a new token repository.
func NewTokenRepo() *TokenRepo {
	return &TokenRepo{
		BaseCatalogRepo: NewBaseCatalogRepo[*token.Token](
			_tokenTable,
			postgres.ExtractDBColumns[token.Token](),
			func() *token.Token { return &token.Token{} },
			false, // flat catalog
		),
	}
}

// FindBySymbolAndNetwork retrieves a token by symbol within a specific network.
func (r *TokenRepo) FindBySymbolAndNetwork(ctx context.Context, symbol string, networkID id.ID) (*token.Token, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"symbol": symbol, "network_id": networkID}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var t token.Token
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &t, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("token", symbol)
		}
		return nil, fmt.Errorf("find by symbol and network: %w", err)
	}

	return &t, nil
}
