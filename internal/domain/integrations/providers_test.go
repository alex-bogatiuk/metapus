package integrations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderPurposesAndCreation(t *testing.T) {
	for _, provider := range []Provider{Alchemy, Infura, TronAPI, Chainstack, GetBlock} {
		require.True(t, provider.Supports(KindRPC))
		require.False(t, provider.Supports(KindExchangeRates))
		require.NoError(t, provider.ValidateCreation())
	}
	require.True(t, CoinGeckoDemo.Supports(KindExchangeRates))
	require.False(t, CoinGeckoDemo.Supports(KindRPC))
	require.NoError(t, CoinGeckoDemo.ValidateCreation())
	for _, provider := range []Provider{Telegram, SMTP, ResourceAPI} {
		require.True(t, provider.Valid())
		require.Error(t, provider.ValidateCreation())
		// Disabling creation does not invalidate saved records.
		account := NewAccount("EXISTING", "Существующий аккаунт")
		account.Provider = provider
		require.NoError(t, account.Validate(context.Background()))
	}
	require.False(t, Provider("missing").Valid())
	require.False(t, Provider("missing").Supports(KindRPC))
	require.Error(t, Provider("missing").ValidateCreation())
}

func TestProviderRegistryIsConsistentAndCannotBeMutatedByCallers(t *testing.T) {
	kinds := map[Kind]bool{}
	for _, kind := range KindDefinitions() {
		require.NotEmpty(t, kind.Label)
		require.False(t, kinds[kind.ID])
		kinds[kind.ID] = true
	}
	seen := map[Provider]bool{}
	for _, provider := range ProviderDefinitions() {
		require.False(t, seen[provider.ID])
		seen[provider.ID] = true
		require.NotEmpty(t, provider.Kinds)
		for _, kind := range provider.Kinds {
			require.True(t, kinds[kind])
		}
	}
	definition, ok := GetBlock.Definition()
	require.True(t, ok)
	definition.Kinds[0] = KindNotifications
	definition.Credentials.Fields[0].Name = "changed"
	original, _ := GetBlock.Definition()
	require.Equal(t, KindRPC, original.Kinds[0])
	require.Equal(t, "fullnode", original.Credentials.Fields[0].Name)
}
