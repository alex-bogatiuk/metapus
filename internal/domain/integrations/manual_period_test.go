package integrations

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func manualFixture(now time.Time) *Account {
	a := NewAccount("MANUAL", "Manual key")
	a.Provider = manualFixtureDefinition.ID
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	a.ManualPeriodStartedAt = &start
	a.CredentialsExpiresAt = &end
	a.ManualPeriodVersion = 1
	return a
}
func TestManualPeriodNeverAdvancesOrRecoversAtExpiry(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	a := manualFixture(now)
	for _, stamp := range []time.Time{now, now.AddDate(0, 1, 0), now.AddDate(1, 0, 0)} {
		start, end, err := a.Period(manualFixtureDefinition, stamp, time.UTC)
		require.NoError(t, err)
		require.Equal(t, *a.ManualPeriodStartedAt, start)
		require.Equal(t, *a.CredentialsExpiresAt, end)
	}
	require.False(t, a.Expired(manualFixtureDefinition, now))
	require.False(t, a.Expired(manualFixtureDefinition, a.CredentialsExpiresAt.Add(-time.Nanosecond)))
	require.True(t, a.Expired(manualFixtureDefinition, *a.CredentialsExpiresAt))
	require.True(t, a.Expired(manualFixtureDefinition, now.AddDate(0, 1, 0)))
	a.ManualPeriodStartedAt = nil
	require.True(t, a.Expired(manualFixtureDefinition, now))
}
func TestManualRenewalRejectsDateOnlyKeyOnlyAndCurrentKey(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	previous := manualFixture(now)
	for _, tc := range []struct {
		name                      string
		replace, same, changeDate bool
		past                      bool
		wantOK                    bool
	}{
		{name: "metadata", wantOK: true}, {name: "date only", changeDate: true}, {name: "key only", replace: true}, {name: "same key", replace: true, same: true, changeDate: true}, {name: "past date", replace: true, changeDate: true, past: true}, {name: "renew", replace: true, changeDate: true, wantOK: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := *previous
			if tc.replace {
				a.PendingCredentials = &Credentials{Keys: []NamedKey{{Name: "sepolia", Value: "new-key"}}}
			}
			if tc.changeDate {
				date := now.AddDate(0, 1, 0)
				if tc.past {
					date = now.Add(-time.Minute)
				}
				a.CredentialsExpiresAt = &date
			}
			err := a.PrepareManualPeriod(manualFixtureDefinition, previous, tc.same, now)
			if !tc.wantOK {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.replace {
				require.EqualValues(t, 2, a.ManualPeriodVersion)
				require.Equal(t, now, *a.ManualPeriodStartedAt)
			} else {
				require.EqualValues(t, 1, a.ManualPeriodVersion)
			}
		})
	}
	a := manualFixture(now)
	a.PendingCredentials = &Credentials{}
	require.NoError(t, a.PrepareManualPeriod(manualFixtureDefinition, previous, false, now))
	require.EqualValues(t, 1, a.ManualPeriodVersion)
	expired := manualFixture(now.AddDate(0, -1, 0))
	require.NoError(t, expired.Validate(context.Background()), "ordinary edits remain possible after expiry")
	require.NoError(t, expired.ValidateFor(manualFixtureDefinition))
}
