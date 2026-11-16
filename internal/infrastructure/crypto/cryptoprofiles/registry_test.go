package cryptoprofiles

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/integrations/profiles"
)

func TestEveryRPCProviderHasAnHTTPProfile(t *testing.T) {
	for _, d := range cryptointegrations.Catalog().Providers() {
		if !cryptointegrations.Catalog().Supports(d.ID, cryptointegrations.KindRPC) {
			continue
		}
		p, ok := Registry().Lookup(d.ID)
		require.True(t, ok, "missing HTTP profile for %s", d.ID)
		require.NotNil(t, p.Authorize)
		require.Equal(t, d.AccountMinRequestIntervalMS, p.MinRequestIntervalMS)
	}
	_, ok := Registry().Lookup(integrations.CoinGeckoDemo)
	require.True(t, ok, "the application registry keeps platform profiles")
	_, ok = (*profiles.Registry)(nil).Lookup(cryptointegrations.Alchemy)
	require.False(t, ok, "the platform registry knows no crypto provider")
}

func TestImportUsesProviderProfilesAndRejectsCredentialExfiltration(t *testing.T) {
	for _, tt := range []struct {
		url, key, role, slot, base string
		provider                   integrations.Provider
	}{
		{"https://eth-sepolia.g.alchemy.com/v2/legacy-key", "", "rpc", "default", "https://eth-sepolia.g.alchemy.com/v2/", cryptointegrations.Alchemy},
		{"https://tron-testnet.g.alchemy.com/v2/legacy-key", "", "fullnode", "nile", "https://tron-testnet.g.alchemy.com/v2/", cryptointegrations.Alchemy},
		{"https://tron-mainnet.g.alchemy.com/v2/", "legacy-key", "solidity", "mainnet", "https://tron-mainnet.g.alchemy.com/v2/", cryptointegrations.Alchemy},
		{"https://eth-sepolia.g.alchemy.com/v2", "file-key", "rpc", "default", "https://eth-sepolia.g.alchemy.com/v2/", cryptointegrations.Alchemy},
		{"https://tron-testnet.g.alchemy.com/v2", "file-key", "fullnode", "nile", "https://tron-testnet.g.alchemy.com/v2/", cryptointegrations.Alchemy},
		{"https://sepolia.infura.io/v3/", "0123456789abcdef0123456789abcdef", "rpc", "sepolia", "https://sepolia.infura.io/v3/", cryptointegrations.Infura},
		{"https://tron-nile.core.chainstack.com/", "opaque-token", "fullnode", "nile", "https://tron-nile.core.chainstack.com/", cryptointegrations.Chainstack},
		{"https://shared.eu-central-1.getblock.io/", "opaque-token", "solidity", "solidity", "https://shared.eu-central-1.getblock.io/", cryptointegrations.GetBlock},
	} {
		provider, base, key, err := ImportRPC(tt.url, tt.key, tt.role, tt.slot)
		require.NoError(t, err)
		require.Equal(t, tt.provider, provider)
		require.Equal(t, tt.base, base)
		require.NotEmpty(t, key)
	}
	for _, raw := range []string{"https://foreign.test/", "https://sepolia.infura.io.evil.test/v3/", "https://sepolia.infura.io/v3/private-token", "https://eth-sepolia.g.alchemy.com/v2/legacy-key?leak=1", "https://eth-sepolia.g.alchemy.com/v2x"} {
		_, _, _, err := ImportRPC(raw, "private-token", "rpc", "default")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-token")
		require.NotContains(t, err.Error(), raw)
	}
}

func TestTronGridPaginationIsPinnedToTheIssuingAccount(t *testing.T) {
	profile, ok := Registry().Lookup(cryptointegrations.TronAPI)
	require.True(t, ok)
	require.Equal(t, &profiles.CursorPin{PathContains: "/v1/", Param: "fingerprint"}, profile.CursorPin)
	require.Equal(t, profile.CursorPin, TronGridCursor(), "anonymous TronGrid endpoints use the same pin")
}

// TRON's {"Error": ...} envelope is a configuration rule: crypto profiles
// classify it, the platform default does not know it.
func TestTRONErrorEnvelopeIsClassifiedByCryptoProfiles(t *testing.T) {
	body := []byte(`{"Error":"monthly request limit exceeded"}`)
	require.Equal(t, integrations.Exhausted, classifyFor(cryptointegrations.TronAPI, 200, body))
	require.Equal(t, integrations.Exhausted, classifyFor(cryptointegrations.Alchemy, 200, body))
	require.Empty(t, profiles.Classify(200, body))
	require.Empty(t, classifyFor(cryptointegrations.TronAPI, 200, []byte(`{"blockID":"x"}`)))
}
