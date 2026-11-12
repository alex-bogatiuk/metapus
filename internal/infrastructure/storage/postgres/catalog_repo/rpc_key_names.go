package catalog_repo

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres"
)

// An endpoint reads one named key from whichever pool account is selected.
// Runtime selection skips an account without that name, so catalog writes keep
// every keyed pool account and every endpoint of the pool consistent. Only key
// names leave this file; values stay in memory.

type rpcKeyHolder struct {
	id        id.ID
	name      string
	encrypted []byte
	keys      []string
}

func (h rpcKeyHolder) keyed() bool { return len(h.encrypted) > 0 }

type rpcKeyUser struct{ name, slot string }

func loadRPCKeyHolders(ctx context.Context, accountIDs []id.ID) ([]rpcKeyHolder, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT id,name,credentials_enc FROM cat_integration_accounts WHERE id=ANY($1) AND NOT deletion_mark AND _deleted_at IS NULL`, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("load pool account keys: %w", err)
	}
	defer rows.Close()
	byID := map[id.ID]rpcKeyHolder{}
	for rows.Next() {
		var h rpcKeyHolder
		if err := rows.Scan(&h.id, &h.name, &h.encrypted); err != nil {
			return nil, fmt.Errorf("scan pool account keys: %w", err)
		}
		byID[h.id] = h
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]rpcKeyHolder, 0, len(accountIDs))
	for _, uid := range accountIDs {
		if h, ok := byID[uid]; ok {
			out = append(out, h)
		}
	}
	return out, decodeRPCKeyNames(out)
}

func decodeRPCKeyNames(holders []rpcKeyHolder) error {
	var key []byte
	for i := range holders {
		if !holders[i].keyed() {
			continue
		}
		if key == nil {
			var err error
			if key, err = IntegrationEncryptionKey(); err != nil {
				return err
			}
		}
		credentials, err := decodeIntegrationCredentials(holders[i].encrypted, key)
		if err != nil {
			return err
		}
		holders[i].keys = make([]string, 0, len(credentials.Keys))
		for _, k := range credentials.Keys {
			holders[i].keys = append(holders[i].keys, k.Name)
		}
	}
	return nil
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

func loadRPCKeyUsers(ctx context.Context, query string, arg any) ([]rpcKeyUser, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("load RPC endpoints of pool: %w", err)
	}
	defer rows.Close()
	var out []rpcKeyUser
	for rows.Next() {
		var u rpcKeyUser
		if err := rows.Scan(&u.name, &u.slot); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// requireRPCKeys rejects a configuration in which a keyed account lacks a key
// that an endpoint of its pool reads. Accounts without credentials are shown
// separately as unconfigured and are not part of this check.
func requireRPCKeys(users []rpcKeyUser, holders []rpcKeyHolder) error {
	for _, u := range users {
		var missing []string
		for _, h := range holders {
			if h.keyed() && !slices.Contains(h.keys, u.slot) {
				missing = append(missing, "«"+h.name+"»")
			}
		}
		if len(missing) > 0 {
			return apperror.NewValidation(fmt.Sprintf("Подключение «%s» использует ключ «%s», которого нет в аккаунтах %s", u.name, u.slot, strings.Join(missing, ", "))).
				WithDetail("reasonCode", "rpc_key_missing").WithDetail("field", "credentialSlot")
		}
	}
	return nil
}

func commonRPCKeyNames(holders []rpcKeyHolder) []string {
	var names []string
	first := true
	for _, h := range holders {
		if !h.keyed() {
			continue
		}
		if first {
			names, first = slices.Clone(h.keys), false
			continue
		}
		names = slices.DeleteFunc(names, func(name string) bool { return !slices.Contains(h.keys, name) })
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// rpcBindingFor resolves the network contract of an endpoint draft.
func rpcBindingFor(ctx context.Context, networkID id.ID, role, url, trustGroup, slot string) (integrations.RPCBinding, error) {
	var family, network string
	if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT chain_family,network_identifier FROM cat_blockchain_networks WHERE id=$1 AND NOT deletion_mark`, networkID).Scan(&family, &network); err != nil {
		return integrations.RPCBinding{}, apperror.NewValidation("Выберите действующую сеть")
	}
	return integrations.RPCBinding{Family: family, NetworkIdentifier: network, Role: role, URL: url, TrustGroup: trustGroup, Slot: slot}, nil
}

// RPCKeyOptions describes the key names an endpoint bound to the pool may use.
func RPCKeyOptions(ctx context.Context, q integrations.RPCKeyQuery) (integrations.RPCKeyOptions, error) {
	out := integrations.RPCKeyOptions{Names: []string{}, Accounts: []integrations.RPCKeyAccount{}}
	var provider integrations.Provider
	if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT provider FROM cat_integration_pools WHERE id=$1 AND NOT deletion_mark AND _deleted_at IS NULL`, q.PoolID).Scan(&provider); err != nil {
		return out, apperror.NewNotFound("integration_pool", q.PoolID.String())
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
	holders, err := loadRPCKeyHolders(ctx, members)
	if err != nil {
		return out, err
	}
	out.Names = commonRPCKeyNames(holders)
	if out.Names == nil {
		out.Names = []string{}
	}
	for _, h := range holders {
		keys := slices.Clone(h.keys)
		if keys == nil {
			keys = []string{}
		}
		slices.Sort(keys)
		out.Accounts = append(out.Accounts, integrations.RPCKeyAccount{ID: h.id.String(), Name: h.name, HasCredentials: h.keyed(), Keys: keys})
	}
	return out, nil
}

// validatePoolRPCKeys checks a new member list against endpoints of the pool.
func validatePoolRPCKeys(ctx context.Context, p *integrations.Pool) error {
	users, err := loadRPCKeyUsers(ctx, `SELECT name,credential_slot FROM sys_crypto_rpc_endpoints WHERE pool_id=$1 AND NOT deletion_mark AND _deleted_at IS NULL ORDER BY name`, p.ID)
	if err != nil || len(users) == 0 {
		return err
	}
	holders, err := loadRPCKeyHolders(ctx, p.AccountIDs)
	if err != nil {
		return err
	}
	return requireRPCKeys(users, holders)
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
	holders := []rpcKeyHolder{{id: a.ID, name: a.Name, encrypted: a.CredentialsEnc}}
	if err := decodeRPCKeyNames(holders); err != nil {
		return err
	}
	return requireRPCKeys(users, holders)
}
