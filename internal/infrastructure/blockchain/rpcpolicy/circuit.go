// Package rpcpolicy contains transport and scheduling mechanisms shared by
// chain adapters. Request semantics and response validation belong to adapters.
package rpcpolicy

import "time"

// Circuit is scoped to one endpoint, role and operation. The caller serializes
// access with the endpoint mutex. A head probe cannot repair a body/index read.
type Circuit struct {
	Failures    int
	OpenUntil   time.Time
	Probing     bool
	Unsupported bool
	LastError   string
	LastOutcome string
	Latency     time.Duration
}

func (c *Circuit) Begin(now time.Time) bool {
	if c.Unsupported || c.Probing || now.Before(c.OpenUntil) {
		return false
	}
	if !c.OpenUntil.IsZero() {
		c.Probing = true
	}
	return true
}

func (c *Circuit) Recoverable(now time.Time) bool {
	return !c.Unsupported && !c.Probing && !c.OpenUntil.IsZero() && !now.Before(c.OpenUntil)
}

// Finish releases the half-open reservation even on caller cancellation or
// admission timeout. Neither event is evidence of a provider failure.
func (c *Circuit) Finish(now time.Time, elapsed time.Duration, category string, parentCanceled bool, threshold int, openFor time.Duration, retryAfter time.Time) {
	c.Probing = false
	if parentCanceled {
		return
	}
	c.LastOutcome = category
	switch category {
	case "", "not_found":
		c.Failures, c.LastError, c.OpenUntil = 0, "", time.Time{}
		if c.Latency == 0 {
			c.Latency = elapsed
		} else {
			c.Latency = (c.Latency*3 + elapsed) / 4
		}
	case "credentials_expired", "query_limit", "invalid_request", "admission_busy", "canceled", "unsupported", "archive_unavailable":
		return // Method capability is tracked separately by the adapter.
	default:
		c.Failures++
		c.LastError = category
		if c.Failures >= threshold || category == "rate_limited" || !c.OpenUntil.IsZero() {
			c.OpenUntil = now.Add(openFor)
		}
	}
	if retryAfter.After(c.OpenUntil) {
		c.OpenUntil = retryAfter
	}
}
