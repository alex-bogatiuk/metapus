//go:build integration

package crypto_integration_repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/catalog_repo"
)

// Before Alchemy fixed its key name, the stand held an Alchemy account whose
// only key was named "nile" and two TRON endpoints reading "nile".
func TestAdoptRPCKeyNamesRenamesSingleLegacyKeys(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	ar, pr := NewIntegrationAccountRepo(), NewIntegrationPoolRepo()
	// The profile before the change let an account name its keys freely.
	legacy := appDefinition(cryptointegrations.Alchemy)
	legacy.Credentials = integrations.CredentialProfile{}
	account := func(code string, keys ...integrations.NamedKey) *integrations.Account {
		a := integrations.NewAccount(code, "Alchemy "+code)
		a.Provider, a.PendingCredentials = cryptointegrations.Alchemy, &integrations.Credentials{Keys: keys}
		require.NoError(t, ar.Prepare(ctx, a, legacy))
		require.NoError(t, ar.Create(ctx, a))
		return a
	}
	pool := func(code string, a *integrations.Account) id.ID {
		p := integrations.NewPool(code, code)
		p.Provider, p.AccountIDs = cryptointegrations.Alchemy, []id.ID{a.ID}
		require.NoError(t, pr.Create(ctx, p))
		return p.ID
	}
	var nile id.ID
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO cat_blockchain_networks(code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES('ADOPT-NILE','Adopt Nile','tron','nile','TRX',6) RETURNING id`).Scan(&nile))
	endpoint := func(code, role string, poolID id.ID, slot string) {
		_, err := db.Exec(ctx, `INSERT INTO sys_crypto_rpc_endpoints(code,name,network_id,role,provider_id,trust_group,url,pool_id,credential_slot) VALUES($1,$1,$2,$3,$1,'alchemy','https://tron-testnet.g.alchemy.com/v2/',$4,$5)`, code, nile, role, poolID, slot)
		require.NoError(t, err)
	}
	slot := func(code string) string {
		var s string
		require.NoError(t, db.QueryRow(ctx, `SELECT credential_slot FROM sys_crypto_rpc_endpoints WHERE code=$1`, code).Scan(&s))
		return s
	}

	single := account("ADOPT-SINGLE", integrations.NamedKey{Name: "nile", Value: "alchemy-nile-key"})
	singlePool := pool("ADOPT-SINGLE", single)
	endpoint("ADOPT-FULL", "fullnode", singlePool, "nile")
	endpoint("ADOPT-SOLID", "solidity", singlePool, "nile")
	several := account("ADOPT-SEVERAL", integrations.NamedKey{Name: "nile", Value: "alchemy-two-1"}, integrations.NamedKey{Name: "sepolia", Value: "alchemy-two-2"})
	endpoint("ADOPT-SEVERAL", "fullnode", pool("ADOPT-SEVERAL", several), "nile")
	before, err := ar.GetByID(ctx, single.ID)
	require.NoError(t, err)

	report, err := AdoptRPCKeyNames(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.RenamedKeys)
	require.Equal(t, 2, report.Endpoints)
	require.Len(t, report.Unresolved, 1)
	require.Contains(t, report.Unresolved[0], "ADOPT-SEVERAL")

	credentials, err := catalog_repo.AccountCredentials(ctx, single.ID)
	require.NoError(t, err)
	require.Equal(t, []integrations.NamedKey{{Name: "default", Value: "alchemy-nile-key"}}, credentials.Keys, "the value survives the rename")
	after, err := ar.GetByID(ctx, single.ID)
	require.NoError(t, err)
	require.Equal(t, before.CredentialsVersion+1, after.CredentialsVersion, "runtimes reload the renamed key")
	require.Equal(t, "default", slot("ADOPT-FULL"))
	require.Equal(t, "default", slot("ADOPT-SOLID"))
	require.Equal(t, "nile", slot("ADOPT-SEVERAL"), "an account with several keys keeps working until the operator fixes it")
	ep, err := NewCryptoRPCEndpointRepo().GetByCode(ctx, "ADOPT-FULL")
	require.NoError(t, err)
	require.NoError(t, NewCryptoRPCEndpointRepo().ValidateBinding(ctx, ep), "the adopted endpoint satisfies the new profile")

	again, err := AdoptRPCKeyNames(ctx)
	require.NoError(t, err)
	require.Zero(t, again.RenamedKeys)
	require.Zero(t, again.Endpoints)
}

func TestRenameAccountKeyRejectsATakenName(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, _ := fixtureDB(t)
	ar := NewIntegrationAccountRepo()
	a := integrations.NewAccount("RENAME-TAKEN", "Rename taken")
	a.Provider = cryptointegrations.Chainstack
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "nile", Value: "token-a"}, {Name: "sepolia", Value: "token-b"}}}
	require.NoError(t, ar.Prepare(ctx, a, appDefinition(a.Provider)))
	require.NoError(t, ar.Create(ctx, a))
	require.Error(t, rename(ctx, a.ID, "nile", "sepolia"))
	renamed, err := catalog_repo.RenameAccountKey(ctx, a.ID, "absent", "other")
	require.NoError(t, err)
	require.False(t, renamed)
}

func rename(ctx context.Context, accountID id.ID, from, to string) error {
	_, err := catalog_repo.RenameAccountKey(ctx, accountID, from, to)
	return err
}
