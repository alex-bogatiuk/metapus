package catalog_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

// The lookups below answer what a configuration asks about integration
// accounts and pools, so its SQL joins its own tables with the IDs they
// return and never names the platform's integration tables. A usable
// account or pool is active, not marked for deletion and not deleted.

// PoolProvider returns the provider of a pool that is not marked for deletion.
func PoolProvider(ctx context.Context, poolID id.ID) (integrations.Provider, error) {
	var provider integrations.Provider
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT provider FROM cat_integration_pools WHERE id=$1 AND NOT deletion_mark AND _deleted_at IS NULL`, poolID).Scan(&provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperror.NewNotFound("integration_pool", poolID.String())
	}
	if err != nil {
		return "", fmt.Errorf("read integration pool provider: %w", err)
	}
	return provider, nil
}

// AccountPoolIDs lists the pools that contain the account.
func AccountPoolIDs(ctx context.Context, accountID id.ID) ([]id.ID, error) {
	return queryIDs(ctx, "list account pools", `SELECT pool_id FROM sys_integration_pool_accounts WHERE account_id=$1 ORDER BY pool_id`, accountID)
}

// PoolsSharingAccounts lists the pools that hold any account of the given
// pool, the pool itself included when it has members.
func PoolsSharingAccounts(ctx context.Context, poolID id.ID) ([]id.ID, error) {
	return queryIDs(ctx, "list pools sharing accounts", `SELECT DISTINCT shared.pool_id FROM sys_integration_pool_accounts own
 JOIN sys_integration_pool_accounts shared ON shared.account_id=own.account_id WHERE own.pool_id=$1 ORDER BY shared.pool_id`, poolID)
}

// AccountProvider returns the provider of an account that is not deleted,
// whatever its activity.
func AccountProvider(ctx context.Context, accountID id.ID) (integrations.Provider, error) {
	var provider integrations.Provider
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT provider FROM cat_integration_accounts WHERE id=$1 AND _deleted_at IS NULL`, accountID).Scan(&provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperror.NewNotFound("integration_account", accountID.String())
	}
	if err != nil {
		return "", fmt.Errorf("read integration account provider: %w", err)
	}
	return provider, nil
}

// ProviderAccountIDs lists the provider's accounts that are not deleted,
// whatever their activity.
func ProviderAccountIDs(ctx context.Context, provider integrations.Provider) ([]id.ID, error) {
	return queryIDs(ctx, "list provider accounts", `SELECT id FROM cat_integration_accounts WHERE provider=$1 AND _deleted_at IS NULL ORDER BY id`, string(provider))
}

// UsableAccountIDs lists the provider's usable accounts.
func UsableAccountIDs(ctx context.Context, provider integrations.Provider) ([]id.ID, error) {
	return queryIDs(ctx, "list usable provider accounts", `SELECT id FROM cat_integration_accounts WHERE provider=$1 AND is_active AND NOT deletion_mark AND _deleted_at IS NULL ORDER BY id`, string(provider))
}

// RequireUsableAccount fails with NotFound unless the account is usable and
// belongs to the provider.
func RequireUsableAccount(ctx context.Context, accountID id.ID, provider integrations.Provider) error {
	return usableAccount(ctx, accountID, provider, "")
}

// LockUsableAccount is RequireUsableAccount that also locks the account row
// until the transaction ends, so it cannot be deactivated or change while the
// caller spends what the account allows. Call it inside a transaction.
func LockUsableAccount(ctx context.Context, accountID id.ID, provider integrations.Provider) error {
	return usableAccount(ctx, accountID, provider, " FOR UPDATE")
}

func usableAccount(ctx context.Context, accountID id.ID, provider integrations.Provider, lock string) error {
	var found id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT id FROM cat_integration_accounts WHERE id=$1 AND provider=$2 AND is_active AND NOT deletion_mark AND _deleted_at IS NULL`+lock, accountID, string(provider)).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NewNotFound("integration_account", accountID.String())
	}
	if err != nil {
		return fmt.Errorf("read integration account: %w", err)
	}
	return nil
}

// CreateKeylessAccount stores an active account without credentials for a
// provider the configuration serves itself, such as an emulator. Accounts
// that hold keys go through IntegrationAccountRepo.
func CreateKeylessAccount(ctx context.Context, accountID id.ID, code, name string, provider integrations.Provider) error {
	if _, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `INSERT INTO cat_integration_accounts(id,code,name,provider) VALUES($1,$2,$3,$4)`, accountID, code, name, string(provider)); err != nil {
		return fmt.Errorf("create integration account: %w", err)
	}
	return nil
}

func queryIDs(ctx context.Context, what, query string, args ...any) ([]id.ID, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer rows.Close()
	out := []id.ID{}
	for rows.Next() {
		var uid id.ID
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		out = append(out, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}
