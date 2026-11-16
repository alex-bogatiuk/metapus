package rpcconfig

import (
	"metapus/internal/domain/crypto/cryptointegrations"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAlchemySpecificHostsSelectTheirNetworkBeforeEVMSuffix(t *testing.T) {
	for _, tt := range []struct {
		give, family, network, role string
	}{
		{"https://eth-sepolia.g.alchemy.com/v2/", "evm", "11155111", "rpc"},
		{"https://tron-testnet.g.alchemy.com/v2/", "tron", "nile", "fullnode"},
		{"https://tron-mainnet.g.alchemy.com/v2", "tron", "mainnet", "solidity"},
	} {
		t.Run(tt.give, func(t *testing.T) {
			profile, ok := RPCProfileForURL(cryptointegrations.Alchemy, tt.give)
			require.True(t, ok)
			require.Equal(t, tt.family, profile.Family)
			binding := RPCBinding{URL: tt.give, Family: tt.family, NetworkIdentifier: tt.network, Role: tt.role, TrustGroup: "alchemy", Slot: "default"}
			require.NoError(t, ValidateRPCBinding(cryptointegrations.Alchemy, binding))
			binding.TrustGroup = "independent-alchemy-account"
			require.Error(t, ValidateRPCBinding(cryptointegrations.Alchemy, binding))
			binding.TrustGroup = "alchemy"
			binding.Family, binding.Role = "evm", "rpc"
			if tt.family == "tron" {
				require.Error(t, ValidateRPCBinding(cryptointegrations.Alchemy, binding))
				binding.Family, binding.Role = "tron", tt.role
				binding.NetworkIdentifier = "shasta"
				require.Error(t, ValidateRPCBinding(cryptointegrations.Alchemy, binding))
				binding.NetworkIdentifier, binding.Role = tt.network, "indexer"
				require.Error(t, ValidateRPCBinding(cryptointegrations.Alchemy, binding))
			} else {
				binding.NetworkIdentifier = "1"
				require.NoError(t, ValidateRPCBinding(cryptointegrations.Alchemy, binding))
			}
		})
	}
}

func TestAlchemyBaseRejectsCredentialsAndMethodPaths(t *testing.T) {
	for _, give := range []string{
		"http://tron-testnet.g.alchemy.com/v2/",
		"https://tron-testnet.g.alchemy.com:444/v2/",
		"https://tron-testnet.g.alchemy.com.evil.test/v2/",
		"https://user:pass@tron-testnet.g.alchemy.com/v2/",
		"https://tron-testnet.g.alchemy.com/v2/private-key",
		"https://tron-testnet.g.alchemy.com/v2/wallet/getnowblock",
		"https://tron-testnet.g.alchemy.com/v2/?",
		"https://tron-testnet.g.alchemy.com/v2/?key=private-key",
		"https://tron-testnet.g.alchemy.com/v2/#fragment",
		"https://tron-testnet.g.alchemy.com/%76%32/",
	} {
		t.Run(give, func(t *testing.T) {
			_, ok := RPCProfileForURL(cryptointegrations.Alchemy, give)
			require.False(t, ok)
		})
	}
}
