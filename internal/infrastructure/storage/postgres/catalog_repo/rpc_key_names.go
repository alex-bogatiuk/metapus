package catalog_repo

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres"
)

// Catalog writes keep every keyed pool account and every endpoint of the pool
// consistent by the rule in integrations.RequireRPCKeys. Only key names leave
// this file; decrypted values stay in memory.

// loadRPCKeyAccounts reads the key names of pool accounts in the given order.
func loadRPCKeyAccounts(ctx context.Context, accountIDs []id.ID) ([]integrations.RPCKeyAccount, error) {
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
	out := make([]integrations.RPCKeyAccount, 0, len(accountIDs))
	for _, uid := range accountIDs {
		s, ok := byID[uid]
		if !ok {
			continue
		}
		account, err := rpcKeyAccount(s.id, s.name, s.encrypted)
		if err != nil {
			return nil, err
		}
		out = append(out, account)
	}
	return out, nil
}

// rpcKeyAccount decrypts the credentials only to list their key names.
func rpcKeyAccount(accountID id.ID, name string, encrypted []byte) (integrations.RPCKeyAccount, error) {
	account := integrations.RPCKeyAccount{ID: accountID.String(), Name: name, HasCredentials: len(encrypted) > 0, Keys: []string{}}
	if !account.HasCredentials {
		return account, nil
	}
	key, err := IntegrationEncryptionKey()
	if err != nil {
		return account, err
	}
	credentials, err := decodeIntegrationCredentials(encrypted, key)
	if err != nil {
		return account, err
	}
	for _, k := range credentials.Keys {
		account.Keys = append(account.Keys, k.Name)
	}
	slices.Sort(account.Keys)
	return account, nil
}

func poolMemberIDs(ctx context.Context, poolID id.ID) ([]id.ID, error) {
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

func loadRPCKeyUsers(ctx context.Context, query string, arg any) ([]integrations.RPCKeyUser, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("load RPC endpoints of pool: %w", err)
	}
	defer rows.Close()
	var out []integrations.RPCKeyUser
	for rows.Next() {
		var u integrations.RPCKeyUser
		if err := rows.Scan(&u.Endpoint, &u.Key); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// rpcBindingFor resolves the network contract of an endpoint draft.
func rpcBindingFor(ctx context.Context, networkID id.ID, role, url, trustGroup, slot string) (integrations.RPCBinding, error) {
	var family, network string
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT chain_family,network_identifier FROM cat_blockchain_networks WHERE id=$1 AND NOT deletion_mark`, networkID).Scan(&family, &network)
	if errors.Is(err, pgx.ErrNoRows) {
		return integrations.RPCBinding{}, apperror.NewValidation("Выберите действующую сеть")
	}
	if err != nil {
		return integrations.RPCBinding{}, fmt.Errorf("read RPC endpoint network: %w", err)
	}
	return integrations.RPCBinding{Family: family, NetworkIdentifier: network, Role: role, URL: url, TrustGroup: trustGroup, Slot: slot}, nil
}

// RPCKeyOptions describes the key names an endpoint bound to the pool may use.
func RPCKeyOptions(ctx context.Context, q integrations.RPCKeyQuery) (integrations.RPCKeyOptions, error) {
	out := integrations.RPCKeyOptions{Names: []string{}, Accounts: []integrations.RPCKeyAccount{}}
	var provider integrations.Provider
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT provider FROM cat_integration_pools WHERE id=$1 AND NOT deletion_mark AND _deleted_at IS NULL`, q.PoolID).Scan(&provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, apperror.NewNotFound("integration_pool", q.PoolID.String())
	}
	if err != nil {
		return out, fmt.Errorf("read integration pool provider: %w", err)
	}
	if !id.IsNil(q.NetworkID) {
		b, err := rpcBindingFor(ctx, q.NetworkID, q.Role, q.URL, "", "")
		if err != nil {
			return out, err
		}
		out.Required = provider.RPCKeyName(b)
	}
	members, err := poolMemberIDs(ctx, q.PoolID)
	if err != nil {
		return out, err
	}
	accounts, err := loadRPCKeyAccounts(ctx, members)
	if err != nil {
		return out, err
	}
	out.Names = integrations.CommonRPCKeyNames(accounts)
	out.Accounts = append(out.Accounts, accounts...)
	return out, nil
}

// validatePoolRPCKeys checks a new member list against endpoints of the pool.
func validatePoolRPCKeys(ctx context.Context, p *integrations.Pool) error {
	users, err := loadRPCKeyUsers(ctx, `SELECT name,credential_slot FROM sys_crypto_rpc_endpoints WHERE pool_id=$1 AND NOT deletion_mark AND _deleted_at IS NULL ORDER BY name`, p.ID)
	if err != nil || len(users) == 0 {
		return err
	}
	accounts, err := loadRPCKeyAccounts(ctx, p.AccountIDs)
	if err != nil {
		return err
	}
	return integrations.RequireRPCKeys(users, accounts)
}

// validateAccountRPCKeys checks new credentials against endpoints of every pool
// that contains the account.
func validateAccountRPCKeys(ctx context.Context, a *integrations.Account) error {
	if a.DeletionMark || len(a.CredentialsEnc) == 0 {
		return nil
	}
	users, err := loadRPCKeyUsers(ctx, `SELECT DISTINCT e.name,e.credential_slot FROM sys_crypto_rpc_endpoints e JOIN sys_integration_pool_accounts m ON m.pool_id=e.pool_id WHERE m.account_id=$1 AND NOT e.deletion_mark AND e._deleted_at IS NULL ORDER BY e.name`, a.ID)
	if err != nil || len(users) == 0 {
		return err
	}
	account, err := rpcKeyAccount(a.ID, a.Name, a.CredentialsEnc)
	if err != nil {
		return err
	}
	return integrations.RequireRPCKeys(users, []integrations.RPCKeyAccount{account})
}
