package automations

import (
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"
)

func TestLatestDueAfterLongOutageAndCalendarBoundary(t *testing.T) {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	for _, tc := range []struct{ expression, first, now, want string }{
		{"* * * * * *", "2020-01-01T00:00:00Z", "2026-10-07T10:00:00Z", "2026-10-07T10:00:00Z"},
		{"0 0 9 * * *", "2026-10-01T09:00:00Z", "2026-10-07T08:59:59Z", "2026-10-06T09:00:00Z"},
		{"0 0 0 29 2 *", "2020-02-29T00:00:00Z", "2026-10-07T10:00:00Z", "2024-02-29T00:00:00Z"},
	} {
		schedule, err := parser.Parse(tc.expression)
		require.NoError(t, err)
		parse := func(value string) time.Time {
			instant, err := time.Parse(time.RFC3339, value)
			require.NoError(t, err)
			return instant
		}
		require.Equal(t, parse(tc.want), LatestDue(schedule, parse(tc.first), parse(tc.now)))
	}
}
