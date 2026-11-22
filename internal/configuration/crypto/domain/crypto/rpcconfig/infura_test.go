package rpcconfig

import (
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/pkg/sdk/integrations"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInfuraSecretsAndDailyQuotaBoundary(t *testing.T) {
	for _, raw := range []string{"https://sepolia.infura.io/v3/", "https://sepolia.infura.io/v3", "https://sepolia.infura.io:443/v3/"} {
		_, ok := RPCProfileForURL(cryptointegrations.Infura, raw)
		require.True(t, ok)
	}
	for _, raw := range []string{"http://sepolia.infura.io/v3/", "https://mainnet.infura.io/v3/", "https://sepolia.infura.io.evil.test/v3/", "https://sepolia.infura.io:8443/v3/", "https://user@sepolia.infura.io/v3/", "https://sepolia.infura.io/v3/key", "https://sepolia.infura.io/v3/?", "https://sepolia.infura.io/v3/#x", "https://sepolia.infura.io/%763/"} {
		_, ok := RPCProfileForURL(cryptointegrations.Infura, raw)
		require.False(t, ok, raw)
	}
	a := integrations.NewAccount("INFURA", "Infura")
	a.Provider, a.ResetDay, a.ResetTimezone = cryptointegrations.Infura, 20, "Europe/Moscow"
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "sepolia", Value: "0123456789abcdef0123456789abcdef"}}}
	definition, _ := cryptointegrations.Catalog().Definition(cryptointegrations.Infura)
	require.NoError(t, a.ValidateFor(definition))
	for _, bad := range []string{"", "key/escape", "https://sepolia.infura.io/v3/0123456789abcdef0123456789abcdef", "0123456789abcdef0123456789abcdeg"} {
		a.PendingCredentials.Keys[0].Value = bad
		require.Error(t, a.ValidateFor(definition))
	}
	for _, stamp := range []string{"2026-10-22T23:59:59Z", "2026-10-23T00:00:00Z", "2026-12-31T23:00:00Z"} {
		now, err := time.Parse(time.RFC3339, stamp)
		require.NoError(t, err)
		start, end, err := a.Period(definition, now, time.UTC)
		require.NoError(t, err)
		require.Equal(t, now.UTC().Truncate(24*time.Hour), start)
		require.Equal(t, start.Add(24*time.Hour), end)
	}
}
