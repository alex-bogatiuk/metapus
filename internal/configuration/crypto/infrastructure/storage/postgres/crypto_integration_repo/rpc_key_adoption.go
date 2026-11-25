package crypto_integration_repo

import (
	"context"
	"fmt"
	"slices"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/catalog_repo"
)

// RPCKeyAdoption reports what AdoptRPCKeyNames changed and what it left for
// the operator. Names only; key values never leave the platform.
type RPCKeyAdoption struct {
	RenamedKeys int      `json:"renamedKeys"`
	Endpoints   int      `json:"endpoints"`
	Unresolved  []string `json:"unresolved"`
}

// AdoptRPCKeyNames brings stored data to the key name a provider profile now
// fixes (for example Alchemy's single app key). An account whose only key has
// another name gets it renamed, and the RPC endpoints of the provider's pools
// read the fixed name. An account with several keys cannot be renamed safely:
// it and its pools' endpoints stay as they are and are reported. Safe to run
// again; it runs in the caller's transaction.
func AdoptRPCKeyNames(ctx context.Context) (RPCKeyAdoption, error) {
	out := RPCKeyAdoption{Unresolved: []string{}}
	if err := catalog_repo.LockIntegrationConfiguration(ctx); err != nil {
		return out, err
	}
	pools, err := rpcEndpointPools(ctx)
	if err != nil {
		return out, err
	}
	for _, definition := range cryptointegrations.Catalog().Providers() {
		name := rpcconfig.AccountKeyName(definition.ID)
		if name == "" || !slices.Contains(definition.Kinds, cryptointegrations.KindRPC) {
			continue
		}
		accountIDs, err := catalog_repo.ProviderAccountIDs(ctx, definition.ID)
		if err != nil {
			return out, err
		}
		accounts, err := catalog_repo.AccountKeyNames(ctx, accountIDs)
		if err != nil {
			return out, err
		}
		for _, a := range accounts {
			if !a.HasCredentials || slices.Contains(a.Keys, name) {
				continue
			}
			if len(a.Keys) != 1 {
				out.Unresolved = append(out.Unresolved, fmt.Sprintf("Аккаунт «%s»: несколько ключей, оставьте один ключ «%s»", a.Name, name))
				continue
			}
			uid, err := id.Parse(a.ID)
			if err != nil {
				return out, fmt.Errorf("parse integration account id: %w", err)
			}
			renamed, err := catalog_repo.RenameAccountKey(ctx, uid, a.Keys[0], name)
			if err != nil {
				return out, err
			}
			if renamed {
				out.RenamedKeys++
			}
		}
		for _, poolID := range pools {
			provider, err := catalog_repo.PoolProvider(ctx, poolID)
			if err != nil || provider != definition.ID {
				continue
			}
			members, err := catalog_repo.PoolAccountKeys(ctx, poolID)
			if err != nil {
				return out, err
			}
			if slices.ContainsFunc(members, func(a integrations.AccountKeys) bool { return a.HasCredentials && !slices.Contains(a.Keys, name) }) {
				// The accounts reported above still lack the name; switching
				// the endpoints now would leave them without a usable key.
				continue
			}
			tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE sys_crypto_rpc_endpoints SET credential_slot=$2,version=version+1 WHERE pool_id=$1 AND credential_slot<>$2 AND _deleted_at IS NULL`, poolID, name)
			if err != nil {
				return out, fmt.Errorf("adopt RPC endpoint key name: %w", err)
			}
			out.Endpoints += int(tag.RowsAffected())
		}
	}
	return out, nil
}

// rpcEndpointPools lists the pools RPC endpoints read through, deleted endpoints aside.
func rpcEndpointPools(ctx context.Context) ([]id.ID, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT DISTINCT pool_id FROM sys_crypto_rpc_endpoints WHERE pool_id IS NOT NULL AND _deleted_at IS NULL ORDER BY pool_id`)
	if err != nil {
		return nil, fmt.Errorf("list RPC endpoint pools: %w", err)
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
