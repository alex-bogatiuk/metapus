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

// FindByContractAndNetwork retrieves a token by its contract address within a specific network.
// If contract is empty, it finds the native token of the network (where contract_address is empty).
func (r *TokenRepo) FindByContractAndNetwork(ctx context.Context, contract string, networkID id.ID) (*token.Token, error) {
	q := r.baseSelect(ctx).
		Where(squirrel.Eq{"contract_address": contract, "network_id": networkID}).
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
			return nil, apperror.NewNotFound("token_contract", contract)
		}
		return nil, fmt.Errorf("find by contract and network: %w", err)
	}

	return &t, nil
}

