package automations

import (
	"context"
	"time"

	"metapus/internal/core/id"
)

// Schedule abstracts a calendar schedule without depending on a cron library.
type Schedule interface{ Next(time.Time) time.Time }

type ScheduledRun struct {
	ID           id.ID
	RuleID       id.ID
	Expression   string
	ScheduledFor time.Time
	ClaimToken   id.ID
	Attempt      int
	MaxAttempts  int
}

type ScheduledRunRepository interface {
	EnqueueDue(context.Context, Rule, Schedule, time.Time) error
	Claim(context.Context, int) ([]ScheduledRun, error)
	Renew(context.Context, ScheduledRun) error
	Finish(context.Context, ScheduledRun, string, error) error
}

// LatestDue finds the last calendar occurrence with logarithmic work, even
// after a long outage of a rule scheduled every second.
func LatestDue(schedule Schedule, first, now time.Time) time.Time {
	if first.After(now) {
		return time.Time{}
	}
	lo, hi := first.Unix()-1, now.Unix()
	for lo+1 < hi {
		mid := lo + (hi-lo)/2
		if next := schedule.Next(time.Unix(mid, 0).UTC()); !next.IsZero() && !next.After(now) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return schedule.Next(time.Unix(lo, 0).UTC())
}
