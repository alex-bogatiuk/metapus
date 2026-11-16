package integrations

import (
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
			start, end, err := a.Period(monthlyFixture, now, mustLocation(t, a.ResetTimezone))
			require.NoError(t, err)
			require.Equal(t, tt.start, start.Format(time.RFC3339))
			require.Equal(t, tt.end, end.Format(time.RFC3339))
		})
	}
}
func TestCredentialSlotsShareAccountAndRejectInvalidInput(t *testing.T) {
	a := NewAccount("a", "Account")
	a.PendingCredentials = &Credentials{Keys: []NamedKey{{Name: "default", Value: "key-a"}, {Name: "sepolia", Value: "key-b"}}}
	require.NoError(t, a.ValidateFor(monthlyFixture))
	key, ok := a.PendingCredentials.Key("")
	require.True(t, ok)
	require.Equal(t, "key-a", key)
	a.PendingCredentials.Keys[1].Name = "default"
	require.Error(t, a.ValidateFor(monthlyFixture))
	a.PendingCredentials.Keys[1].Name = "other"
	a.PendingCredentials.Keys[1].Value = "unsafe\nheader"
	require.Error(t, a.ValidateFor(monthlyFixture))
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	return loc
}
