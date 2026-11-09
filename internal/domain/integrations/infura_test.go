package integrations

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInfuraSecretsAndDailyQuotaBoundary(t *testing.T) {
	for _, raw := range []string{"https://sepolia.infura.io/v3/", "https://sepolia.infura.io/v3", "https://sepolia.infura.io:443/v3/"} {
		_, ok := Infura.RPCProfileForURL(raw)
		require.True(t, ok)
	}
	for _, raw := range []string{"http://sepolia.infura.io/v3/", "https://mainnet.infura.io/v3/", "https://sepolia.infura.io.evil.test/v3/", "https://sepolia.infura.io:8443/v3/", "https://user@sepolia.infura.io/v3/", "https://sepolia.infura.io/v3/key", "https://sepolia.infura.io/v3/?", "https://sepolia.infura.io/v3/#x", "https://sepolia.infura.io/%763/"} {
		_, ok := Infura.RPCProfileForURL(raw)
		require.False(t, ok, raw)
	}
	a := NewAccount("INFURA", "Infura")
	a.Provider, a.ResetDay, a.ResetTimezone = Infura, 20, "Europe/Moscow"
	a.PendingCredentials = &Credentials{Keys: []NamedKey{{Name: "sepolia", Value: "0123456789abcdef0123456789abcdef"}}}
	require.NoError(t, a.Validate(t.Context()))
	for _, bad := range []string{"", "key/escape", "https://sepolia.infura.io/v3/0123456789abcdef0123456789abcdef", "0123456789abcdef0123456789abcdeg"} {
		a.PendingCredentials.Keys[0].Value = bad
		require.Error(t, a.Validate(t.Context()))
	}
	for _, stamp := range []string{"2026-10-22T23:59:59Z", "2026-10-23T00:00:00Z", "2026-12-31T23:00:00Z"} {
		now, err := time.Parse(time.RFC3339, stamp)
		require.NoError(t, err)
		start, end, err := a.Period(now, mustLocation(t, a.ResetTimezone))
		require.NoError(t, err)
		require.Equal(t, now.UTC().Truncate(24*time.Hour), start)
		require.Equal(t, start.Add(24*time.Hour), end)
	}
}
