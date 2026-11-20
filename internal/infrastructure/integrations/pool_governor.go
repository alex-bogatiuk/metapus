package integrations

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/providerpolicy"
)

// A pool governor paces one tenant pool in this process after the provider
// rate-limits it. Account pacers keep their fixed intervals; the governor
// adds a pool-wide rate that halves on a 429 and grows back linearly, so a
// rotation to the next account does not resend at the rate that was just
// refused. A 429 on a second account shortly after the first means the limit
// is shared (typically per IP): the pool pauses until Retry-After instead of
// marking every account in turn.
const (
	// governorSharedWindow is how soon a 429 on another account counts as one
	// shared limit rather than two independent account limits.
	governorSharedWindow = 10 * time.Second
	// governorCutHold merges the 429s of one burst into a single rate cut.
	governorCutHold = time.Second
	// governorShortThrottle bounds the throttles the governor reacts to. Longer
	// ones are daily or billing limits: rotating accounts is the right answer.
	governorShortThrottle = 5 * time.Minute
	// governorMinRate keeps a governed pool usable after repeated cuts.
	governorMinRate = 0.5
	// governorRecoveryRate is the linear growth, in requests per second, per
	// second without a 429.
	governorRecoveryRate = 1.0
	// governorReleaseAfter lifts the limit once it has stayed well above the
	// measured demand for this long without a 429.
	governorReleaseAfter = time.Minute
	// governorRateWindow is the span the send rate is measured over.
	governorRateWindow = 2 * time.Second
)

var poolThrottles = func() metric.Int64Counter {
	counter, _ := otel.Meter("metapus/integrations").Int64Counter("integration.pool.throttles")
	return counter
}()

var governors = struct {
	sync.Mutex
	entries map[string]*poolGovernor
}{entries: make(map[string]*poolGovernor)}

func governorFor(ctx context.Context, poolID id.ID) *poolGovernor {
	key := tenant.GetTenantID(ctx) + "/" + poolID.String()
	governors.Lock()
	defer governors.Unlock()
	g := governors.entries[key]
	if g == nil {
		g = &poolGovernor{reported: make(map[id.ID]time.Time)}
		governors.entries[key] = g
	}
	return g
}

type poolGovernor struct {
	mu sync.Mutex
	// limit is the pool rate in requests per second; zero means no limit.
	limit       float64
	next        time.Time // Earliest send under limit.
	pausedUntil time.Time
	adjustedAt  time.Time
	cutAt       time.Time
	throttledAt time.Time
	throttledBy id.ID
	sends       []time.Time // Sends within governorRateWindow, oldest first.
	reported    map[id.ID]time.Time
}

// Wait reserves the pool's next send. A send that cannot start before the
// caller's deadline fails at once with admission busy, so the caller can use
// another provider instead of waiting out its whole attempt.
func (g *poolGovernor) Wait(ctx context.Context, now time.Time) error {
	g.mu.Lock()
	g.recover(now)
	at := now
	if g.pausedUntil.After(at) {
		at = g.pausedUntil
	}
	if g.limit > 0 {
		if g.next.After(at) {
			at = g.next
		}
	}
	if deadline, ok := ctx.Deadline(); ok && at.Sub(now) > time.Until(deadline) {
		g.mu.Unlock()
		return errors.Join(providerpolicy.ErrAdmissionBusy, context.DeadlineExceeded)
	}
	if g.limit > 0 {
		g.next = at.Add(time.Duration(float64(time.Second) / g.limit))
	}
	g.record(at)
	g.mu.Unlock()
	if delay := at.Sub(now); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return errors.Join(providerpolicy.ErrAdmissionBusy, ctx.Err())
		case <-timer.C:
		}
	}
	return nil
}

// Throttled records a 429 of account and reports whether the limit is
// shared by the pool's accounts. A long throttle is left to account rotation.
func (g *poolGovernor) Throttled(account id.ID, retry, now time.Time) (shared bool) {
	if retry.Sub(now) > governorShortThrottle {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.recover(now)
	shared = !id.IsNil(g.throttledBy) && g.throttledBy != account && now.Sub(g.throttledAt) < governorSharedWindow
	if now.Sub(g.cutAt) >= governorCutHold {
		base := g.rate(now)
		if g.limit > 0 && g.limit < base {
			base = g.limit
		}
		g.limit = max(governorMinRate, base/2)
		g.cutAt, g.adjustedAt = now, now
		if g.next.Before(now) {
			g.next = now
		}
	}
	g.throttledAt, g.throttledBy = now, account
	if shared && retry.After(g.pausedUntil) {
		g.pausedUntil = retry
	}
	return shared
}

// ShouldReport deduplicates the account-state write of one throttle: requests
// already in flight on that account report the same 429 again.
func (g *poolGovernor) ShouldReport(account id.ID, retry, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if until, ok := g.reported[account]; ok && now.Before(until) {
		return false
	}
	for other, until := range g.reported {
		if !now.Before(until) {
			delete(g.reported, other)
		}
	}
	g.reported[account] = retry
	return true
}

// recover grows the limit linearly and lifts it once it is far above demand.
func (g *poolGovernor) recover(now time.Time) {
	if g.limit == 0 {
		return
	}
	if now.Sub(g.throttledAt) < governorCutHold {
		g.adjustedAt = now // No growth while the provider is still refusing.
	} else if elapsed := now.Sub(g.adjustedAt); elapsed > 0 {
		g.limit += elapsed.Seconds() * governorRecoveryRate
		g.adjustedAt = now
	}
	if now.Sub(g.throttledAt) >= governorReleaseAfter && g.limit >= 2*g.rate(now) {
		g.limit, g.next = 0, time.Time{}
	}
}

func (g *poolGovernor) record(at time.Time) {
	g.sends = append(g.sends, at)
	g.trim(at)
}

func (g *poolGovernor) trim(now time.Time) {
	drop := 0
	for drop < len(g.sends) && now.Sub(g.sends[drop]) > governorRateWindow {
		drop++
	}
	g.sends = g.sends[drop:]
}

// rate is the measured send rate in requests per second.
func (g *poolGovernor) rate(now time.Time) float64 {
	g.trim(now)
	return float64(len(g.sends)) / governorRateWindow.Seconds()
}

func recordPoolThrottle(ctx context.Context, provider model.Provider, shared bool) {
	scope := "account"
	if shared {
		scope = "shared"
	}
	poolThrottles.Add(ctx, 1, metric.WithAttributes(attribute.String("integration.provider", string(provider)), attribute.String("integration.throttle_scope", scope)))
}
