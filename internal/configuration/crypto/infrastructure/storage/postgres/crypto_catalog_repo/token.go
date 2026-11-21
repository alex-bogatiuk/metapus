package crypto_catalog_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

// TokenRepo implements token.Repository.
type TokenRepo struct {
	*catalog_repo.BaseCatalogRepo[*token.Token]
}

// NewTokenRepo creates a new token repository.
func NewTokenRepo() *TokenRepo {
	return &TokenRepo{
		BaseCatalogRepo: catalog_repo.NewCatalogRepoForDefinition[*token.Token](token.Definition,
			postgres.ExtractDBColumns[token.Token](),
			func() *token.Token { return &token.Token{} }),
	}
}

// IdentityForUpdate serializes catalog edits with the first financial use.
func (r *TokenRepo) IdentityForUpdate(ctx context.Context, tokenID id.ID) (token.Identity, bool, error) {
	var identity token.Identity
	var locked bool
	q := r.GetTxManager(ctx).GetQuerier(ctx)
	err := q.QueryRow(ctx, `
		SELECT network_id, contract_address, decimal_places, token_standard,
		       financial_identity_locked
		FROM cat_tokens WHERE id = $1 FOR UPDATE
	`, tokenID).Scan(&identity.NetworkID, &identity.ContractAddress,
		&identity.DecimalPlaces, &identity.Standard, &locked)
	if err != nil {
		return token.Identity{}, false, fmt.Errorf("lock token identity: %w", err)
	}
	return identity, locked, nil
}

// FindBySymbolAndNetwork retrieves a token by symbol within a specific network.
func (r *TokenRepo) FindBySymbolAndNetwork(ctx context.Context, symbol string, networkID id.ID) (*token.Token, error) {
	q := r.BaseSelect(ctx).
		Where(squirrel.Eq{"symbol": symbol, "network_id": networkID}).
		Where(squirrel.Eq{"deletion_mark": false}).
		Limit(1)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var t token.Token
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
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
	q := r.BaseSelect(ctx).
		Where(squirrel.Eq{"contract_address": contract, "network_id": networkID}).
		Where(squirrel.Expr("((deletion_mark=FALSE AND is_active=TRUE) OR id IN (SELECT owner_id FROM sys_crypto_watch_targets WHERE network_id=? AND target_kind='asset' AND target_key=?))", networkID, contract)).
		Limit(1)
	if contract == "" {
		q = q.Where(squirrel.Eq{"token_standard": token.TokenStandardNative})
	} else {
		q = q.Where(squirrel.NotEq{"token_standard": token.TokenStandardNative})
	}

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var t token.Token
	querier := r.GetTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Get(ctx, querier, &t, sql, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("token_contract", contract)
		}
		return nil, fmt.Errorf("find by contract and network: %w", err)
	}

	return &t, nil
}
