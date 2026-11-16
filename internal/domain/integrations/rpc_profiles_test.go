package integrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRPCProviderContractsOwnValidationAndTrustIdentity(t *testing.T) {
	for _, definition := range ProviderDefinitions() {
		if !definition.ID.Supports(KindRPC) {
			continue
		}
		require.NotEmpty(t, definition.RPCProfiles)
		for _, profile := range definition.RPCProfiles {
			host := profile.Host
			if host == "" {
				host = "eth-sepolia" + profile.HostSuffix
			}
			base := "https://" + host + profile.BasePath + "/"
			b := RPCBinding{Family: profile.Family, NetworkIdentifier: profile.NetworkIdentifier, Role: profile.Roles[0], URL: base, TrustGroup: profile.TrustGroup, Slot: profile.Roles[0]}
			if name := definition.ID.RPCKeyName(b); name != "" {
				b.Slot = "another-key"
				require.Error(t, definition.ID.ValidateRPCBinding(b))
				b.Slot = name
			}
			require.NoError(t, definition.ID.ValidateRPCBinding(b))
			require.Equal(t, profile.TrustGroup, RPCTrustIdentity(base, "fake-independent-vote", "arbitrary-name"))
			b.URL = base + "secret"
			require.Error(t, definition.ID.ValidateRPCBinding(b))
			b.URL = base
			b.TrustGroup = "fake-independent-vote"
			require.Error(t, definition.ID.ValidateRPCBinding(b))
		}
	}
	definition, _ := GetBlock.Definition()
	definition.RPCProfiles[0].Roles[0] = "corrupted"
	definition.Credentials.URLImports[0].RolePaths["fullnode"] = "corrupted"
	fresh, _ := GetBlock.Definition()
	require.Equal(t, "fullnode", fresh.RPCProfiles[0].Roles[0])
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
		provider Provider
		binding  RPCBinding
		want     string
	}{
		{Infura, sepolia("https://sepolia.infura.io/v3/"), "sepolia"},
		{DRPC, sepolia("https://lb.drpc.live/sepolia/"), "sepolia"},
		{Chainstack, sepolia("https://ethereum-sepolia.core.chainstack.com/"), "sepolia"},
		{Chainstack, nile("https://tron-nile.core.chainstack.com/", "fullnode"), "nile"},
		{GetBlock, nile("https://shared.eu-central-1.getblock.io/", "solidity"), "solidity"},
		// Alchemy accounts may hold several keys; the operator picks one.
		{Alchemy, sepolia("https://eth-sepolia.g.alchemy.com/v2/"), ""},
		{Alchemy, nile("https://tron-testnet.g.alchemy.com/v2/", "fullnode"), ""},
		// An unsupported binding has no profile and therefore no fixed name.
		{Infura, nile("https://sepolia.infura.io/v3/", "fullnode"), ""},
	} {
		require.Equal(t, tt.want, tt.provider.RPCKeyName(tt.binding), "%s %s", tt.provider, tt.binding.URL)
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
