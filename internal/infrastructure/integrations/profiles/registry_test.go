package profiles

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	model "metapus/internal/domain/integrations"
)

func TestEveryManagedRPCProviderHasAnHTTPProfile(t *testing.T) {
	for _, d := range model.ProviderDefinitions() {
		if !d.ID.Supports(model.KindRPC) {
			continue
		}
		p, ok := (*Registry)(nil).Lookup(d.ID)
		require.True(t, ok, "missing HTTP profile for %s", d.ID)
		require.NotNil(t, p.Authorize)
		require.Equal(t, d.AccountMinRequestIntervalMS, p.MinRequestIntervalMS())
	}
}

func TestRegistryRejectsDuplicatesAndDoesNotExposeMutableProbeDefinitions(t *testing.T) {
	authorize := func(*http.Request, string) error { return nil }
	_, err := New(Profile{Provider: "new", Authorize: authorize}, Profile{Provider: "new", Authorize: authorize})
	require.Error(t, err)
	_, err = New(Profile{Provider: "new"})
	require.Error(t, err)
	p := Profile{Provider: "new", Authorize: authorize, Probe: &Probe{URL: "https://provider.test/ping"}}
	r, err := New(p)
	require.NoError(t, err)
	p.Probe.URL = "https://foreign.test"
	got, ok := r.Lookup("new")
	require.True(t, ok)
	require.Equal(t, "https://provider.test/ping", got.Probe.URL)
	got.Probe.URL = "https://foreign.test"
	again, _ := r.Lookup("new")
	require.Equal(t, "https://provider.test/ping", again.Probe.URL)
	_, ok = r.Lookup("missing")
	require.False(t, ok)
}

func TestImportUsesProviderProfilesAndRejectsCredentialExfiltration(t *testing.T) {
	for _, tt := range []struct {
		url, key, role, slot, base string
		provider                   model.Provider
	}{
		{"https://eth-sepolia.g.alchemy.com/v2/legacy-key", "", "rpc", "default", "https://eth-sepolia.g.alchemy.com/v2/", model.Alchemy},
		{"https://tron-testnet.g.alchemy.com/v2/legacy-key", "", "fullnode", "nile", "https://tron-testnet.g.alchemy.com/v2/", model.Alchemy},
		{"https://tron-mainnet.g.alchemy.com/v2/", "legacy-key", "solidity", "mainnet", "https://tron-mainnet.g.alchemy.com/v2/", model.Alchemy},
		{"https://sepolia.infura.io/v3/", "0123456789abcdef0123456789abcdef", "rpc", "sepolia", "https://sepolia.infura.io/v3/", model.Infura},
		{"https://tron-nile.core.chainstack.com/", "opaque-token", "fullnode", "nile", "https://tron-nile.core.chainstack.com/", model.Chainstack},
		{"https://shared.eu-central-1.getblock.io/", "opaque-token", "solidity", "solidity", "https://shared.eu-central-1.getblock.io/", model.GetBlock},
	} {
		provider, base, key, err := (*Registry)(nil).ImportRPC(tt.url, tt.key, tt.role, tt.slot)
		require.NoError(t, err)
		require.Equal(t, tt.provider, provider)
		require.Equal(t, tt.base, base)
		require.NotEmpty(t, key)
	}
	for _, raw := range []string{"https://foreign.test/", "https://sepolia.infura.io.evil.test/v3/", "https://sepolia.infura.io/v3/private-token", "https://eth-sepolia.g.alchemy.com/v2/legacy-key?leak=1"} {
		_, _, _, err := (*Registry)(nil).ImportRPC(raw, "private-token", "rpc", "default")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-token")
		require.NotContains(t, err.Error(), raw)
	}
}
