package cryptointegrations

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/domain/integrations"
)

func TestProviderPurposesAndCreation(t *testing.T) {
	catalog := Catalog()
	for _, provider := range []integrations.Provider{Alchemy, Ankr, ValidationCloud, ZAN, Infura, TronAPI, Chainstack, GetBlock, DRPC} {
		require.True(t, catalog.Supports(provider, KindRPC))
		require.False(t, catalog.Supports(provider, integrations.KindExchangeRates))
		require.NoError(t, catalog.ValidateCreation(provider))
	}
	require.True(t, catalog.Supports(integrations.CoinGeckoDemo, integrations.KindExchangeRates), "the application catalog keeps platform providers")
	require.False(t, catalog.Supports(integrations.CoinGeckoDemo, KindRPC))
	for _, provider := range []integrations.Provider{integrations.Telegram, integrations.SMTP, ResourceAPI, TestnetAML} {
		_, err := catalog.Require(provider)
		require.NoError(t, err)
		require.Error(t, catalog.ValidateCreation(provider))
	}
}

func TestCatalogKeepsTheSettingsListingOrder(t *testing.T) {
	ids := func(providers []integrations.ProviderDefinition) []integrations.Provider {
		out := make([]integrations.Provider, len(providers))
		for i, p := range providers {
			out[i] = p.ID
		}
		return out
	}
	require.Equal(t, providerOrder, ids(Catalog().Providers()))
	kinds := []integrations.Kind{}
	for _, kind := range Catalog().Kinds() {
		kinds = append(kinds, kind.ID)
	}
	require.Equal(t, kindOrder, kinds)
}

// The configuration migration constrains stored providers to this catalog.
func TestMigrationProviderChecksMatchTheCatalog(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", "..", "db", "configuration", "crypto", "00006_crypto_core.sql"))
	require.NoError(t, err)
	want := []string{}
	for _, provider := range Catalog().Providers() {
		want = append(want, string(provider.ID))
	}
	slices.Sort(want)
	checks := regexp.MustCompile(`ck_integration_(?:accounts|pools)_provider CHECK \(provider IN \(([^)]*)\)\)`).FindAllStringSubmatch(string(raw), -1)
	require.Len(t, checks, 2)
	for _, check := range checks {
		got := strings.Split(strings.NewReplacer("'", "", " ", "").Replace(check[1]), ",")
		slices.Sort(got)
		require.Equal(t, want, got)
	}
	for _, provider := range Catalog().Providers() {
		require.Equal(t, provider.ID == DRPC, provider.ManualPeriod(), "the migration allows a manual key period for dRPC only")
	}
}

func TestGetBlockReplacesBothRolesOrExplicitlyClearsCredentials(t *testing.T) {
	for _, tt := range []struct {
		name        string
		credentials *integrations.Credentials
		valid       bool
	}{
		{"keep saved keys", nil, true},
		{"explicit clear", &integrations.Credentials{Keys: []integrations.NamedKey{}}, true},
		{"both roles", &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "fullnode", Value: "fullnode-token"}, {Name: "solidity", Value: "solidity-token"}}}, true},
		{"fullnode alone", &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "fullnode", Value: "fullnode-token"}}}, false},
		{"solidity alone", &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "solidity", Value: "solidity-token"}}}, false},
		{"default is not a role", &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "default", Value: "token"}}}, false},
		{"empty token", &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "fullnode", Value: "fullnode-token"}, {Name: "solidity", Value: ""}}}, false},
		{"URL is not a token", &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "fullnode", Value: "https://shared.eu-central-1.getblock.io/token/"}, {Name: "solidity", Value: "solidity-token"}}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := integrations.NewAccount("GETBLOCK", "GetBlock TRON")
			a.Provider = GetBlock
			definition, _ := Catalog().Definition(GetBlock)
			a.PendingCredentials = tt.credentials
			err := a.ValidateFor(definition)
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestGetBlockNetHasSeparateCredentialsAndNoCalendarReset(t *testing.T) {
	account := integrations.NewAccount("AML", "Getblock.net")
	account.Provider = GetBlockNet
	require.True(t, Catalog().Supports(GetBlockNet, KindAML))
	require.False(t, Catalog().Supports(GetBlockNet, KindRPC))
	definition, _ := Catalog().Definition(GetBlockNet)
	require.Equal(t, integrations.QuotaPackage, definition.QuotaPolicy)
	require.NotEmpty(t, definition.PoolSelectionDenied, "AML accounts are pinned, never selected from a pool")
	for _, keys := range [][]integrations.NamedKey{
		{{Name: "default", Value: "opaque.bearer-token"}},
		{{Name: "fullnode", Value: "rpc-key"}},
		{{Name: "default", Value: "https://example.test/token"}},
		{{Name: "default", Value: "Bearer token"}},
		{{Name: "default", Value: "token"}, {Name: "solidity", Value: "rpc-key"}},
	} {
		account.PendingCredentials = &integrations.Credentials{Keys: keys}
		err := account.ValidateFor(definition)
		if len(keys) == 1 && keys[0].Value == "opaque.bearer-token" {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
	_, _, err := account.Period(definition, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	require.ErrorContains(t, err, "не имеет календарного сброса")
}

func TestDRPCSingleKeyCredentials(t *testing.T) {
	d, _ := Catalog().Definition(DRPC)
	require.NoError(t, d.Credentials.Validate(integrations.Credentials{Keys: []integrations.NamedKey{{Name: "sepolia", Value: "opaque_123-key"}}}))
	require.Error(t, d.Credentials.Validate(integrations.Credentials{Keys: []integrations.NamedKey{{Name: "default", Value: "opaque"}}}))
	require.Error(t, d.Credentials.Validate(integrations.Credentials{Keys: []integrations.NamedKey{{Name: "sepolia", Value: "secret/segment"}}}))
}
