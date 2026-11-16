package crypto_integration_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

// RPCPoolConsumer keeps every keyed pool account and every endpoint of the
// pool consistent by the rule in rpcconfig.RequireRPCKeys. Only key names
// leave this file; decrypted values stay in memory.
type RPCPoolConsumer struct{}

var _ integrations.PoolConsumer = RPCPoolConsumer{}

// UsesPool reports whether an active RPC endpoint reads through the pool.
func (RPCPoolConsumer) UsesPool(ctx context.Context, poolID id.ID) (bool, error) {
	var used bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_rpc_endpoints WHERE pool_id=$1 AND NOT deletion_mark)`, poolID).Scan(&used)
	return used, err
}

// NewIntegrationAccountRepo is the account repository of this configuration:
// credential changes are checked against RPC endpoints of the account's pools.
func NewIntegrationAccountRepo() *catalog_repo.IntegrationAccountRepo {
	return catalog_repo.NewIntegrationAccountRepo(RPCPoolConsumer{})
}

// NewIntegrationPoolRepo is the pool repository of this configuration: a pool
// used by RPC endpoints keeps their key names and cannot be deleted.
func NewIntegrationPoolRepo() *catalog_repo.IntegrationPoolRepo {
	return catalog_repo.NewIntegrationPoolRepo(RPCPoolConsumer{})
}

func loadRPCKeyUsers(ctx context.Context, query string, arg any) ([]rpcconfig.RPCKeyUser, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("load RPC endpoints of pool: %w", err)
	}
	defer rows.Close()
	var out []rpcconfig.RPCKeyUser
	for rows.Next() {
		var u rpcconfig.RPCKeyUser
		if err := rows.Scan(&u.Endpoint, &u.Key); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// rpcBindingFor resolves the network contract of an endpoint draft.
func rpcBindingFor(ctx context.Context, networkID id.ID, role, url, trustGroup, slot string) (rpcconfig.RPCBinding, error) {
	var family, network string
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT chain_family,network_identifier FROM cat_blockchain_networks WHERE id=$1 AND NOT deletion_mark`, networkID).Scan(&family, &network)
	if errors.Is(err, pgx.ErrNoRows) {
		return rpcconfig.RPCBinding{}, apperror.NewValidation("Выберите действующую сеть")
	}
	if err != nil {
		return rpcconfig.RPCBinding{}, fmt.Errorf("read RPC endpoint network: %w", err)
	}
	return rpcconfig.RPCBinding{Family: family, NetworkIdentifier: network, Role: role, URL: url, TrustGroup: trustGroup, Slot: slot}, nil
}

// RPCKeyOptions describes the key names an endpoint bound to the pool may use.
func (RPCPoolConsumer) RPCKeyOptions(ctx context.Context, q rpcconfig.RPCKeyQuery) (rpcconfig.RPCKeyOptions, error) {
	out := rpcconfig.RPCKeyOptions{Names: []string{}, Accounts: []integrations.AccountKeys{}}
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
		out.Required = rpcconfig.RPCKeyName(provider, b)
	}
	members, err := catalog_repo.PoolMemberIDs(ctx, q.PoolID)
	if err != nil {
		return out, err
	}
	accounts, err := catalog_repo.AccountKeyNames(ctx, members)
	if err != nil {
		return out, err
	}
	out.Names = rpcconfig.CommonRPCKeyNames(accounts)
	out.Accounts = append(out.Accounts, accounts...)
	return out, nil
}

// ValidatePool checks a new member list against endpoints of the pool.
func (RPCPoolConsumer) ValidatePool(ctx context.Context, p *integrations.Pool) error {
	users, err := loadRPCKeyUsers(ctx, `SELECT name,credential_slot FROM sys_crypto_rpc_endpoints WHERE pool_id=$1 AND NOT deletion_mark AND _deleted_at IS NULL ORDER BY name`, p.ID)
	if err != nil || len(users) == 0 {
		return err
	}
	accounts, err := catalog_repo.AccountKeyNames(ctx, p.AccountIDs)
	if err != nil {
		return err
	}
	return rpcconfig.RequireRPCKeys(users, accounts)
}

// ValidateAccount checks new credentials against endpoints of every pool
// that contains the account.
func (RPCPoolConsumer) ValidateAccount(ctx context.Context, a *integrations.Account) error {
	users, err := loadRPCKeyUsers(ctx, `SELECT DISTINCT e.name,e.credential_slot FROM sys_crypto_rpc_endpoints e JOIN sys_integration_pool_accounts m ON m.pool_id=e.pool_id WHERE m.account_id=$1 AND NOT e.deletion_mark AND e._deleted_at IS NULL ORDER BY e.name`, a.ID)
	if err != nil || len(users) == 0 {
		return err
	}
	account, err := catalog_repo.AccountKeyNamesOf(a.ID, a.Name, a.CredentialsEnc)
	if err != nil {
		return err
	}
	return rpcconfig.RequireRPCKeys(users, []integrations.AccountKeys{account})
}
