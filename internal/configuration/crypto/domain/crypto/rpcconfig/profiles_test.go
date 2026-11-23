package rpcconfig

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRPCProviderContractsOwnValidationAndTrustIdentity(t *testing.T) {
	for _, definition := range cryptointegrations.Catalog().Providers() {
		if !cryptointegrations.Catalog().Supports(definition.ID, cryptointegrations.KindRPC) {
			continue
		}
		require.NotEmpty(t, RPCProfiles(definition.ID))
		for _, profile := range RPCProfiles(definition.ID) {
			host := profile.Host
			if host == "" {
				host = "eth-sepolia" + profile.HostSuffix
			}
			base := "https://" + host + profile.BasePath + "/"
			b := RPCBinding{Family: profile.Family, NetworkIdentifier: profile.NetworkIdentifier, Role: profile.Roles[0], URL: base, TrustGroup: profile.TrustGroup, Slot: profile.Roles[0]}
			if name := RPCKeyName(definition.ID, b); name != "" {
				b.Slot = "another-key"
				require.Error(t, ValidateRPCBinding(definition.ID, b))
				b.Slot = name
			}
			require.NoError(t, ValidateRPCBinding(definition.ID, b))
			require.Equal(t, profile.TrustGroup, RPCTrustIdentity(base, "fake-independent-vote", "arbitrary-name"))
			b.URL = base + "secret"
			require.Error(t, ValidateRPCBinding(definition.ID, b))
			b.URL = base
			b.TrustGroup = "fake-independent-vote"
			require.Error(t, ValidateRPCBinding(definition.ID, b))
		}
	}
	definition, _ := cryptointegrations.Catalog().Definition(cryptointegrations.GetBlock)
	RPCProfiles(definition.ID)[0].Roles[0] = "corrupted"
	definition.Credentials.URLImports[0].RolePaths["fullnode"] = "corrupted"
	fresh, _ := cryptointegrations.Catalog().Definition(cryptointegrations.GetBlock)
	require.Equal(t, "fullnode", RPCProfiles(fresh.ID)[0].Roles[0])
	require.Equal(t, "wallet", fresh.Credentials.URLImports[0].RolePaths["fullnode"])
}

func TestRPCKeyNameFollowsProviderProfile(t *testing.T) {
	sepolia := func(url string) RPCBinding {
		return RPCBinding{Family: "evm", NetworkIdentifier: "11155111", Role: "rpc", URL: url}
	}
	nile := func(url, role string) RPCBinding {
		return RPCBinding{Family: "tron", NetworkIdentifier: "nile", Role: role, URL: url}
	}
	for _, tt := range []struct {
		provider integrations.Provider
		binding  RPCBinding
		want     string
	}{
		{cryptointegrations.Infura, sepolia("https://sepolia.infura.io/v3/"), "sepolia"},
		{cryptointegrations.DRPC, sepolia("https://lb.drpc.live/sepolia/"), "sepolia"},
		{cryptointegrations.Chainstack, sepolia("https://ethereum-sepolia.core.chainstack.com/"), "sepolia"},
		{cryptointegrations.Chainstack, nile("https://tron-nile.core.chainstack.com/", "fullnode"), "nile"},
		{cryptointegrations.GetBlock, nile("https://shared.eu-central-1.getblock.io/", "solidity"), "solidity"},
		// cryptointegrations.Alchemy accounts may hold several keys; the operator picks one.
		{cryptointegrations.Alchemy, sepolia("https://eth-sepolia.g.alchemy.com/v2/"), ""},
		{cryptointegrations.Alchemy, nile("https://tron-testnet.g.alchemy.com/v2/", "fullnode"), ""},
		// An unsupported binding has no profile and therefore no fixed name.
		{cryptointegrations.Infura, nile("https://sepolia.infura.io/v3/", "fullnode"), ""},
	} {
		require.Equal(t, tt.want, RPCKeyName(tt.provider, tt.binding), "%s %s", tt.provider, tt.binding.URL)
	}
}

func TestRPCTrustIdentityRetainsFileOnlyHostNormalization(t *testing.T) {
	for _, operator := range []struct{ host, group string }{
		{"g.alchemy.com", "alchemy"}, {"infura.io", "infura"}, {"tenderly.co", "tenderly"},
		{"publicnode.com", "publicnode"}, {"onfinality.io", "onfinality"}, {"trongrid.io", "trongrid"},
		{"nileex.io", "nileex"}, {"core.chainstack.com", "chainstack"},
	} {
		require.Equal(t, operator.group, RPCTrustIdentity("https://"+operator.host, "fake-vote", "endpoint-name"))
		require.Equal(t, operator.group, RPCTrustIdentity("https://rpc."+operator.host, "fake-vote", "endpoint-name"))
		require.Equal(t, "custom", RPCTrustIdentity("https://"+operator.host+".foreign.test", "custom", "endpoint-name"))
	}
}

func TestDRPCDeclarativeBindingAndSingleKey(t *testing.T) {
	binding := RPCBinding{Family: "evm", NetworkIdentifier: "11155111", Role: "rpc", URL: "https://lb.drpc.live/sepolia/", TrustGroup: "drpc", Slot: "sepolia"}
	require.NoError(t, ValidateRPCBinding(cryptointegrations.DRPC, binding))
	for _, mutate := range []func(*RPCBinding){func(b *RPCBinding) { b.NetworkIdentifier = "1" }, func(b *RPCBinding) { b.Role = "indexer" }, func(b *RPCBinding) { b.TrustGroup = "second-vote" }, func(b *RPCBinding) { b.URL += "secret" }, func(b *RPCBinding) { b.Slot = "default" }, func(b *RPCBinding) { b.URL = "https://lb.drpc.live.evil.test/sepolia/" }} {
		bad := binding
		mutate(&bad)
		require.Error(t, ValidateRPCBinding(cryptointegrations.DRPC, bad))
	}
	require.Equal(t, "drpc", RPCTrustIdentity(binding.URL, "another", "arbitrary-name"))
}

func TestProjectProvidersRejectUnsupportedNetworksAndRoles(t *testing.T) {
	const zanNile = "https://api.zan.top/node/v1/tron/nile"
	for _, tt := range []struct {
		provider                    integrations.Provider
		base, family, network, role string
	}{
		{cryptointegrations.ValidationCloud, "https://sepolia.ethereum.validationcloud.io/v1", "evm", "1", "rpc"},
		{cryptointegrations.ZAN, zanNile, "tron", "mainnet", "fullnode"},
		{cryptointegrations.ZAN, zanNile, "tron", "nile", "indexer"},
		{cryptointegrations.ZAN, zanNile, "evm", "11155111", "rpc"},
		{cryptointegrations.ZAN, "https://api.zan.top/node/v1/eth/sepolia", "tron", "nile", "solidity"},
	} {
		require.True(t, ManagedRPCOnly(tt.provider))
		b := RPCBinding{Family: tt.family, NetworkIdentifier: tt.network, Role: tt.role, URL: tt.base, TrustGroup: string(tt.provider)}
		b.Slot = RPCKeyName(tt.provider, b)
		require.Error(t, ValidateRPCBinding(tt.provider, b))
	}
}

func TestAutoRoutedGatewayCannotIncreaseIndependentCoverage(t *testing.T) {
	group := RPCVotingGroup("https://api.uniblock.dev/uni/v1/json-rpc", "my-node", "alias")
	require.Empty(t, group)
	rows := []RPCConfigurationEndpoint{
		{Role: "rpc", IsActive: true, TrustGroup: group},
		{Role: "rpc", IsActive: true, TrustGroup: "direct"},
	}
	require.Equal(t, 1, RPCRequirements("evm", "production", rows)[0].ConfiguredProviders)
}
