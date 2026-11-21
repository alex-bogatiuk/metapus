package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

type ProviderOpeningAuthorityRepo struct{}

func NewProviderOpeningAuthorityRepo() *ProviderOpeningAuthorityRepo {
	return &ProviderOpeningAuthorityRepo{}
}

func (r *ProviderOpeningAuthorityRepo) LockSystemAdmin(ctx context.Context, actor id.ID) (cryptoapp.ProviderOpeningAdminAuthority, error) {
	if postgres.MustGetTxManager(ctx).GetTx(ctx) == nil {
		return cryptoapp.ProviderOpeningAdminAuthority{}, fmt.Errorf("provider opening authorization requires a command transaction")
	}
	return r.read(ctx, actor, true)
}

func (r *ProviderOpeningAuthorityRepo) ReadSystemAdmin(ctx context.Context, actor id.ID) (cryptoapp.ProviderOpeningAdminAuthority, error) {
	return r.read(ctx, actor, false)
}

func (*ProviderOpeningAuthorityRepo) read(ctx context.Context, actor id.ID, lock bool) (cryptoapp.ProviderOpeningAdminAuthority, error) {
	var authority cryptoapp.ProviderOpeningAdminAuthority
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx,
		`SELECT authority_basis,user_auth_version FROM crypto_provider_opening_admin_authority($1,$2)`, actor, lock).Scan(&authority.Basis, &authority.AuthVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return authority, nil
	}
	return authority, err
}

var _ cryptoapp.ProviderOpeningAuthorityRepository = (*ProviderOpeningAuthorityRepo)(nil)
