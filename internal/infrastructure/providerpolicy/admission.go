package providerpolicy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type Capacity struct{ Concurrency, BulkConcurrency, RequestsPerSecond, MinRequestIntervalMS int }

// CanCompleteSequence rejects a route whose configured minimum spacing alone
// exceeds the operation deadline. It does not reserve slots or promise capacity.
func (a *Admission) CanCompleteSequence(ctx context.Context, requests int) bool {
	if ctx.Err() != nil {
		return false
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || requests <= 1 || a.capacity.MinRequestIntervalMS == 0 {
		return true
	}
	minimum := time.Duration(requests-1) * time.Duration(a.capacity.MinRequestIntervalMS) * time.Millisecond
	return minimum < time.Until(deadline)
}

// Admission belongs to a process endpoint, shared across tenants and roles.
// A smaller bulk semaphore leaves capacity for urgent state/receipt reads.
type Admission struct {
	all, bulk      chan struct{}
	limiter        *rate.Limiter
	capacity       Capacity
	references     int
	mu             sync.Mutex
	throttledUntil time.Time
}

var admissions = struct {
	sync.Mutex
	entries map[[32]byte]*Admission
}{entries: make(map[[32]byte]*Admission)}

func Acquire(endpoint string, c Capacity) (*Admission, error) {
	if c.Concurrency < 2 || c.BulkConcurrency < 1 || c.BulkConcurrency >= c.Concurrency || c.RequestsPerSecond < 1 || c.MinRequestIntervalMS < 0 || c.MinRequestIntervalMS > 30000 {
		return nil, errors.New("invalid RPC admission capacity")
	}
	key := sha256.Sum256([]byte(endpoint)) // Never retain a credential-bearing URL as a map key.
	admissions.Lock()
	defer admissions.Unlock()
	if a := admissions.entries[key]; a != nil {
		if a.capacity != c {
			return nil, errors.New("conflicting process-wide RPC provider admission settings")
		}
		a.references++
		return a, nil
	}
	limit, burst := rate.Limit(c.RequestsPerSecond), c.Concurrency
	if c.MinRequestIntervalMS > 0 {
		limit, burst = min(limit, rate.Every(time.Duration(c.MinRequestIntervalMS)*time.Millisecond)), 1
	}
	a := &Admission{all: make(chan struct{}, c.Concurrency), bulk: make(chan struct{}, c.BulkConcurrency), limiter: rate.NewLimiter(limit, burst), capacity: c, references: 1}
	admissions.entries[key] = a
	return a, nil
}

func Release(a *Admission) {
	admissions.Lock()
	defer admissions.Unlock()
	a.references--
	if a.references == 0 {
		for k, entry := range admissions.entries {
			if entry == a {
				delete(admissions.entries, k)
				break
			}
		}
	}
}

// Wait honors the endpoint's throttle and request rate without taking another
// concurrency slot. Use it for additional wire requests within an admitted
// operation, such as subdivisions of a provider-limited logs range.
func (a *Admission) Wait(ctx context.Context) error {
	// Honor endpoint-wide Retry-After before consuming capacity. An attempt's
	// deadline includes this wait, so another provider can satisfy the read.
	for {
		a.mu.Lock()
		until := a.throttledUntil
		a.mu.Unlock()
		delay := time.Until(until)
		if delay <= 0 {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return a.limiter.Wait(ctx)
}

func (a *Admission) Enter(ctx context.Context, bulk bool) (func(), error) {
	if err := a.Wait(ctx); err != nil {
		return nil, err
	}
	if bulk {
		select {
		case a.bulk <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	select {
	case a.all <- struct{}{}:
		return func() {
			<-a.all
			if bulk {
				<-a.bulk
			}
		}, nil
	case <-ctx.Done():
		if bulk {
			<-a.bulk
		}
		return nil, ctx.Err()
	}
}

func (a *Admission) Throttle(until time.Time) {
	a.mu.Lock()
	if until.After(a.throttledUntil) {
		a.throttledUntil = until
	}
	a.mu.Unlock()
}

// AcquireScoped keeps tenant pools with the same secret-free URL independent.
// Capacity is part of the generation identity so new settings can be prepared
// while the old generation drains; unchanged settings retain shared admission.
func AcquireScoped(scope, endpoint string, c Capacity) (*Admission, error) {
	if scope == "" {
		return Acquire(endpoint, c)
	}
	return Acquire(fmt.Sprintf("%s/%s/%d/%d/%d/%d", scope, endpoint, c.Concurrency, c.BulkConcurrency, c.RequestsPerSecond, c.MinRequestIntervalMS), c)
}
