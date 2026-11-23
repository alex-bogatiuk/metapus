package catalog_repo

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

// AccountKeyNames reads the key names, never values, of accounts in the given order.
func AccountKeyNames(ctx context.Context, accountIDs []id.ID) ([]integrations.AccountKeys, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT id,name,credentials_enc FROM cat_integration_accounts WHERE id=ANY($1) AND NOT deletion_mark AND _deleted_at IS NULL`, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("load pool account keys: %w", err)
	}
	defer rows.Close()
	type stored struct {
		id        id.ID
		name      string
		encrypted []byte
	}
	byID := map[id.ID]stored{}
	for rows.Next() {
		var s stored
		if err := rows.Scan(&s.id, &s.name, &s.encrypted); err != nil {
			return nil, fmt.Errorf("scan pool account keys: %w", err)
		}
		byID[s.id] = s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]integrations.AccountKeys, 0, len(accountIDs))
	for _, uid := range accountIDs {
		s, ok := byID[uid]
		if !ok {
			continue
		}
		account, err := AccountKeyNamesOf(s.id, s.name, s.encrypted)
		if err != nil {
			return nil, err
		}
		out = append(out, account)
	}
	return out, nil
}

// AccountKeyNamesOf decrypts the credentials only to list their key names.
func AccountKeyNamesOf(accountID id.ID, name string, encrypted []byte) (integrations.AccountKeys, error) {
	account := integrations.AccountKeys{ID: accountID.String(), Name: name, HasCredentials: len(encrypted) > 0, Keys: []string{}}
	if !account.HasCredentials {
		return account, nil
	}
	credentials, err := OpenCredentials(encrypted)
	if err != nil {
		return account, err
	}
	for _, k := range credentials.Keys {
		account.Keys = append(account.Keys, k.Name)
	}
	slices.Sort(account.Keys)
	return account, nil
}

// PoolMemberIDs lists pool accounts in selection order.
func PoolMemberIDs(ctx context.Context, poolID id.ID) ([]id.ID, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT account_id FROM sys_integration_pool_accounts WHERE pool_id=$1 ORDER BY position`, poolID)
	if err != nil {
		return nil, fmt.Errorf("load pool members: %w", err)
	}
	defer rows.Close()
	var out []id.ID
	for rows.Next() {
		var uid id.ID
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

// PoolAccountKeys reads the key names, never values, of the pool's accounts
// in selection order.
func PoolAccountKeys(ctx context.Context, poolID id.ID) ([]integrations.AccountKeys, error) {
	members, err := PoolMemberIDs(ctx, poolID)
	if err != nil {
		return nil, err
	}
	return AccountKeyNames(ctx, members)
}

// AccountKeysOf lists the key names of an account's credentials as they are
// about to be written, so a pool consumer can check them before the write.
func AccountKeysOf(account *integrations.Account) (integrations.AccountKeys, error) {
	return AccountKeyNamesOf(account.ID, account.Name, account.CredentialsEnc)
}

// AccountCredentials opens the stored credentials of an integration account
// for a caller that talks to the account's provider itself. The platform key
// stays in the platform; a deleted account has none.
func AccountCredentials(ctx context.Context, accountID id.ID) (integrations.Credentials, error) {
	var sealed []byte
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT credentials_enc FROM cat_integration_accounts WHERE id=$1 AND _deleted_at IS NULL`, accountID).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return integrations.Credentials{}, apperror.NewNotFound("integration_account", accountID.String())
	}
	if err != nil {
		return integrations.Credentials{}, fmt.Errorf("load integration account credentials: %w", err)
	}
	if len(sealed) == 0 {
		return integrations.Credentials{}, nil
	}
	return OpenCredentials(sealed)
}
