package dto

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/catalogs/rate_source"
	"metapus/internal/domain/integrations"
	"metapus/internal/metadata"
)

func TestIntegrationAccountNeverSerializesSecretMaterial(t *testing.T) {
	a := integrations.NewAccount("A", "Alchemy A")
	a.CredentialsEnc = []byte("ciphertext-must-not-leak")
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "default", Value: "plaintext-must-not-leak"}}}
	for _, value := range []any{a, FromIntegrationAccount(a), []*integrations.Account{a}} {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "plaintext")
		require.NotContains(t, string(raw), "ciphertext")
		require.NotContains(t, string(raw), "credentialsEnc")
		require.NotContains(t, string(raw), "pendingCredentials")
	}
	def := metadata.Inspect(integrations.Account{}, "IntegrationAccount", metadata.TypeCatalog)
	raw, err := json.Marshal(def)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "credentialsEnc")
	require.NotContains(t, string(raw), "pendingCredentials")
	require.True(t, FromIntegrationAccount(a).HasCredentials)
}

func TestLegacyUpdateDoesNotReenableFileCredentials(t *testing.T) {
	uid := id.New()
	s := rate_source.NewRateSource("CG", "CoinGecko", "coingecko")
	s.IntegrationPoolID = &uid
	update := UpdateRateSourceRequest{}
	update.ApplyTo(s)
	require.Equal(t, &uid, s.IntegrationPoolID)
}
