//go:build integration

package crypto_integration_repo

import (
	"errors"
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
)

func requireKeyError(t *testing.T, err error, reason string) {
	t.Helper()
	var appErr *apperror.AppError
	require.True(t, errors.As(err, &appErr), "%v", err)
	require.Equal(t, reason, appErr.Details["reasonCode"], appErr.Message)
	require.Equal(t, "credentialSlot", appErr.Details["field"])
}

// The test stand had an Infura endpoint reading key "default" while its
// accounts, created through the account form, held key "sepolia". Runtime
// selection then skipped every account and reported a transport failure.
func TestRPCKeyNameFollowsProfileAndPoolAccounts(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	ar, pr, er := NewIntegrationAccountRepo(), NewIntegrationPoolRepo(), NewCryptoRPCEndpointRepo()
	account := func(code string, provider integrations.Provider, keys ...integrations.NamedKey) *integrations.Account {
		a := integrations.NewAccount(code, code)
		a.Provider = provider
		if len(keys) > 0 {
			a.PendingCredentials = &integrations.Credentials{Keys: keys}
		}
		require.NoError(t, a.Validate(ctx))
		require.NoError(t, ar.Prepare(ctx, a, appDefinition(a.Provider)))
		require.NoError(t, ar.Create(ctx, a))
		return a
	}
	var sepolia, nile id.ID
	require.NoError(t, db.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE code='ETHEREUM-SEPOLIA'`).Scan(&sepolia))
	require.NoError(t, db.QueryRow(ctx, `INSERT INTO cat_blockchain_networks(code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES('KEYS-TEST-NILE','Keys test Nile','tron','nile','TRX',6) RETURNING id`).Scan(&nile))

	// Infura: the profile fixes the name; an omitted or default name becomes it.
	infura := account("INFURA-KEYS", cryptointegrations.Infura, integrations.NamedKey{Name: "sepolia", Value: "0123456789abcdef0123456789abcdef"})
	infuraPool := integrations.NewPool("INFURA-KEYS", "Infura")
	infuraPool.Provider, infuraPool.AccountIDs = cryptointegrations.Infura, []id.ID{infura.ID}
	require.NoError(t, pr.Create(ctx, infuraPool))
	e := rpcconfig.NewRPCEndpoint("INFURA-KEYS-RPC", "ETHEREUM-SEPOLIA/rpc/infura-sepolia")
	e.NetworkID, e.ProviderID, e.TrustGroup = sepolia, "infura-sepolia", "infura"
	e.URL, e.PoolID = "https://sepolia.infura.io/v3/", &infuraPool.ID
	require.Equal(t, "default", e.Slot)
	require.NoError(t, er.Create(ctx, e))
	var stored string
	require.NoError(t, db.QueryRow(ctx, `SELECT credential_slot FROM sys_crypto_rpc_endpoints WHERE id=$1`, e.ID).Scan(&stored))
	require.Equal(t, "sepolia", stored)
	wrong := *e
	wrong.Slot = "mainnet"
	err := er.ValidateBinding(ctx, &wrong)
	var appErr *apperror.AppError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, "rpc_binding_invalid", appErr.Details["reasonCode"])

	options, err := RPCPoolConsumer{}.RPCKeyOptions(ctx, rpcconfig.RPCKeyQuery{PoolID: infuraPool.ID, NetworkID: sepolia, Role: "rpc", URL: e.URL})
	require.NoError(t, err)
	require.Equal(t, "sepolia", options.Required)
	require.Equal(t, []string{"sepolia"}, options.Names)
	require.Equal(t, []string{"sepolia"}, options.Accounts[0].Keys)

	// Pool members and credentials cannot drop the key the endpoint reads.
	stray := account("INFURA-STRAY", cryptointegrations.Infura, integrations.NamedKey{Name: "default", Value: "fedcba9876543210fedcba9876543210"})
	infuraPool.AccountIDs = []id.ID{infura.ID, stray.ID}
	requireKeyError(t, pr.ValidateMembers(ctx, infuraPool), "rpc_key_missing")
	unconfigured := account("INFURA-EMPTY", cryptointegrations.Infura)
	infuraPool.AccountIDs = []id.ID{infura.ID, unconfigured.ID}
	require.NoError(t, pr.ValidateMembers(ctx, infuraPool), "an account without credentials is shown as unconfigured, not rejected")

	current, err := ar.GetByID(ctx, infura.ID)
	require.NoError(t, err)
	current.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "default", Value: "00112233445566778899aabbccddeeff"}}}
	require.NoError(t, ar.Prepare(ctx, current, appDefinition(current.Provider)))
	requireKeyError(t, ar.Update(ctx, current), "rpc_key_missing")

	// Alchemy accounts may hold several keys; the operator chooses a common one.
	first := account("ALCHEMY-KEYS-1", cryptointegrations.Alchemy, integrations.NamedKey{Name: "nile", Value: "alchemy-nile-1"}, integrations.NamedKey{Name: "sepolia", Value: "alchemy-sepolia-1"})
	second := account("ALCHEMY-KEYS-2", cryptointegrations.Alchemy, integrations.NamedKey{Name: "nile", Value: "alchemy-nile-2"})
	alchemyPool := integrations.NewPool("ALCHEMY-KEYS", "Alchemy")
	alchemyPool.Provider, alchemyPool.AccountIDs = cryptointegrations.Alchemy, []id.ID{first.ID, second.ID}
	require.NoError(t, pr.Create(ctx, alchemyPool))
	options, err = RPCPoolConsumer{}.RPCKeyOptions(ctx, rpcconfig.RPCKeyQuery{PoolID: alchemyPool.ID, NetworkID: nile, Role: "fullnode", URL: "https://tron-testnet.g.alchemy.com/v2/"})
	require.NoError(t, err)
	require.Empty(t, options.Required)
	require.Equal(t, []string{"nile"}, options.Names)
	a := rpcconfig.NewRPCEndpoint("ALCHEMY-KEYS-RPC", "TRON-NILE/fullnode/alchemy-nile")
	a.NetworkID, a.Role, a.ProviderID, a.TrustGroup = nile, "fullnode", "alchemy-nile", "alchemy"
	a.URL, a.PoolID, a.Slot = "https://tron-testnet.g.alchemy.com/v2/", &alchemyPool.ID, "sepolia"
	requireKeyError(t, er.Create(ctx, a), "rpc_key_missing")
	a.Slot = "nile"
	require.NoError(t, er.Create(ctx, a))

	// Anonymous endpoints carry no account key.
	public := rpcconfig.NewRPCEndpoint("PUBLIC-KEYS-RPC", "ETHEREUM-SEPOLIA/rpc/public")
	public.NetworkID, public.ProviderID, public.TrustGroup = sepolia, "public-keys", "public-keys"
	public.URL, public.Slot = "https://rpc.example.invalid/", "anything"
	require.NoError(t, er.Create(ctx, public))
	require.NoError(t, db.QueryRow(ctx, `SELECT credential_slot FROM sys_crypto_rpc_endpoints WHERE id=$1`, public.ID).Scan(&stored))
	require.Equal(t, "default", stored)
}
