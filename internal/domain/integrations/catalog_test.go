package integrations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Fixture providers stand in for a configuration's providers.
var (
	monthlyFixture          = ProviderDefinition{ID: "fixture_monthly", Label: "Monthly", Kinds: []Kind{KindExchangeRates}, CanCreate: true, QuotaPolicy: QuotaMonthly}
	manualFixtureDefinition = ProviderDefinition{ID: "fixture_manual", Label: "Manual", Kinds: []Kind{KindExchangeRates}, CanCreate: true, QuotaPolicy: QuotaManual,
		Credentials: CredentialProfile{Fields: []CredentialField{{Name: "sepolia"}}, KeyFormat: CredentialURLSafe, RequireFields: true, SingleKey: true, KeyOnly: true}}
)

func TestPlatformCatalogPurposesAndCreation(t *testing.T) {
	catalog := PlatformCatalog()
	require.True(t, catalog.Supports(CoinGeckoDemo, KindExchangeRates))
	require.False(t, catalog.Supports(CoinGeckoDemo, KindNotifications))
	require.NoError(t, catalog.ValidateCreation(CoinGeckoDemo))
	for _, provider := range []Provider{Telegram, SMTP} {
		definition, err := catalog.Require(provider)
		require.NoError(t, err)
		require.Error(t, catalog.ValidateCreation(provider))
		// Disabling creation does not invalidate saved records.
		account := NewAccount("EXISTING", "Существующий аккаунт")
		account.Provider = provider
		require.NoError(t, account.Validate(context.Background()))
		require.NoError(t, account.ValidateFor(definition))
	}
	_, err := catalog.Require("missing")
	require.Error(t, err)
	require.False(t, catalog.Supports("missing", KindExchangeRates))
	require.Error(t, catalog.ValidateCreation("missing"))
}

func TestCatalogAcceptsConfigurationProvidersAndRejectsInconsistentOnes(t *testing.T) {
	extra := KindDefinition{ID: "fixture_kind", Label: "Fixture"}
	provider := ProviderDefinition{ID: "fixture_provider", Kinds: []Kind{extra.ID}, CanCreate: true, QuotaPolicy: QuotaMonthly}
	catalog, err := NewCatalog(append(PlatformKinds(), extra), append(PlatformProviders(), provider))
	require.NoError(t, err)
	require.True(t, catalog.Supports(provider.ID, extra.ID))
	require.Len(t, catalog.Providers(), len(PlatformProviders())+1)
	require.Equal(t, extra, catalog.Kinds()[len(catalog.Kinds())-1])

	for name, build := range map[string]func() error{
		"duplicate kind": func() error { _, err := NewCatalog([]KindDefinition{extra, extra}, nil); return err },
		"empty kind":     func() error { _, err := NewCatalog([]KindDefinition{{}}, nil); return err },
		"duplicate provider": func() error {
			_, err := NewCatalog([]KindDefinition{extra}, []ProviderDefinition{provider, provider})
			return err
		},
		"unknown kind": func() error { _, err := NewCatalog(PlatformKinds(), []ProviderDefinition{provider}); return err },
		"invalid identifier": func() error {
			bad := provider
			bad.ID = "Bad-ID"
			_, err := NewCatalog([]KindDefinition{extra}, []ProviderDefinition{bad})
			return err
		},
	} {
		require.Error(t, build(), name)
	}
}

func TestCatalogCannotBeMutatedByCallers(t *testing.T) {
	provider := ProviderDefinition{ID: "fixture_roles", Kinds: []Kind{KindExchangeRates}, Credentials: CredentialProfile{
		Fields:     []CredentialField{{Name: "fullnode"}},
		URLImports: []CredentialURLImport{{Host: "fixture.test", RolePaths: map[string]string{"fullnode": "wallet"}}},
	}}
	input := []ProviderDefinition{provider}
	catalog := MustCatalog(PlatformKinds(), input)
	input[0].Credentials.Fields[0].Name = "changed-before"
	definition, ok := catalog.Definition(provider.ID)
	require.True(t, ok)
	definition.Kinds[0] = KindNotifications
	definition.Credentials.Fields[0].Name = "changed"
	definition.Credentials.URLImports[0].RolePaths["fullnode"] = "changed"
	catalog.Providers()[0].Credentials.Fields[0].Name = "changed-again"
	original, _ := catalog.Definition(provider.ID)
	require.Equal(t, KindExchangeRates, original.Kinds[0])
	require.Equal(t, "fullnode", original.Credentials.Fields[0].Name)
	require.Equal(t, "wallet", original.Credentials.URLImports[0].RolePaths["fullnode"])
}
