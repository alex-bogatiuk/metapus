package integrations

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountQuotaPeriods(t *testing.T) {
	tests := []struct {
		name, now, zone string
		day             int
		start, end      string
	}{
		{"calendar", "2026-10-19T10:00:00Z", "UTC", 1, "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z"},
		{"billing before anchor", "2026-10-05T10:00:00Z", "UTC", 15, "2026-09-15T00:00:00Z", "2026-10-15T00:00:00Z"},
		{"short month", "2026-02-28T00:00:00Z", "UTC", 31, "2026-02-28T00:00:00Z", "2026-03-31T00:00:00Z"},
		{"leap month", "2024-02-28T23:59:59Z", "UTC", 31, "2024-01-31T00:00:00Z", "2024-02-29T00:00:00Z"},
		{"local month boundary", "2026-10-31T22:00:00Z", "Europe/Moscow", 1, "2026-10-31T21:00:00Z", "2026-11-30T21:00:00Z"},
		{"DST", "2026-03-20T10:00:00Z", "America/New_York", 1, "2026-03-01T05:00:00Z", "2026-04-01T04:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAccount("a", "Account")
			a.ResetDay = tt.day
			a.ResetTimezone = tt.zone
			now, err := time.Parse(time.RFC3339, tt.now)
			require.NoError(t, err)
			start, end, err := a.Period(now, mustLocation(t, a.ResetTimezone))
			require.NoError(t, err)
			require.Equal(t, tt.start, start.Format(time.RFC3339))
			require.Equal(t, tt.end, end.Format(time.RFC3339))
		})
	}
}
func TestCredentialSlotsShareAccountAndRejectInvalidInput(t *testing.T) {
	a := NewAccount("a", "Account")
	a.PendingCredentials = &Credentials{Keys: []NamedKey{{Name: "default", Value: "key-a"}, {Name: "sepolia", Value: "key-b"}}}
	require.NoError(t, a.Validate(context.Background()))
	key, ok := a.PendingCredentials.Key("")
	require.True(t, ok)
	require.Equal(t, "key-a", key)
	a.PendingCredentials.Keys[1].Name = "default"
	require.Error(t, a.Validate(context.Background()))
	a.PendingCredentials.Keys[1].Name = "other"
	a.PendingCredentials.Keys[1].Value = "unsafe\nheader"
	require.Error(t, a.Validate(context.Background()))
}

func TestGetBlockReplacesBothRolesOrExplicitlyClearsCredentials(t *testing.T) {
	for _, tt := range []struct {
		name        string
		credentials *Credentials
		valid       bool
	}{
		{"keep saved keys", nil, true},
		{"explicit clear", &Credentials{Keys: []NamedKey{}}, true},
		{"both roles", &Credentials{Keys: []NamedKey{{Name: "fullnode", Value: "fullnode-token"}, {Name: "solidity", Value: "solidity-token"}}}, true},
		{"fullnode alone", &Credentials{Keys: []NamedKey{{Name: "fullnode", Value: "fullnode-token"}}}, false},
		{"solidity alone", &Credentials{Keys: []NamedKey{{Name: "solidity", Value: "solidity-token"}}}, false},
		{"default is not a role", &Credentials{Keys: []NamedKey{{Name: "default", Value: "token"}}}, false},
		{"empty token", &Credentials{Keys: []NamedKey{{Name: "fullnode", Value: "fullnode-token"}, {Name: "solidity", Value: ""}}}, false},
		{"URL is not a token", &Credentials{Keys: []NamedKey{{Name: "fullnode", Value: "https://shared.eu-central-1.getblock.io/token/"}, {Name: "solidity", Value: "solidity-token"}}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAccount("GETBLOCK", "GetBlock TRON")
			a.Provider = GetBlock
			a.PendingCredentials = tt.credentials
			err := a.Validate(context.Background())
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestGetBlockNetHasSeparateCredentialsAndNoCalendarReset(t *testing.T) {
	account := NewAccount("AML", "Getblock.net")
	account.Provider = GetBlockNet
	require.True(t, GetBlockNet.Supports(KindAML))
	require.False(t, GetBlockNet.Supports(KindRPC))
	definition, _ := GetBlockNet.Definition()
	require.Equal(t, QuotaPackage, definition.QuotaPolicy)
	for _, keys := range [][]NamedKey{
		{{Name: "default", Value: "opaque.bearer-token"}},
		{{Name: "fullnode", Value: "rpc-key"}},
		{{Name: "default", Value: "https://example.test/token"}},
		{{Name: "default", Value: "Bearer token"}},
		{{Name: "default", Value: "token"}, {Name: "solidity", Value: "rpc-key"}},
	} {
		account.PendingCredentials = &Credentials{Keys: keys}
		err := account.Validate(t.Context())
		if len(keys) == 1 && keys[0].Value == "opaque.bearer-token" {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
	_, _, err := account.Period(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	require.ErrorContains(t, err, "не имеет календарного сброса")
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	return loc
}
