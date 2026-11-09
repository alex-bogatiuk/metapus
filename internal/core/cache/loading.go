// Package cache provides bounded in-process caching without infrastructure dependencies.
package cache

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

type entry[K comparable, V any] struct {
	key       K
	value     V
	expiresAt time.Time
}

type load[V any] struct {
	done        chan struct{}
	value       V
	err         error
	invalidated bool
}

// Loading coalesces loads per key and retains at most maxEntries values.
// Entries expire after a fixed TTL; eviction under capacity pressure is FIFO.
// Expired entries are removed on reads and writes, without a background worker.
// Values must be immutable after publication. Loader errors are never cached.
type Loading[K comparable, V any] struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
	entries    map[K]*list.Element
	order      list.List // oldest insertion (and earliest expiration) first
	loads      map[K]*load[V]
}

func NewLoading[K comparable, V any](ttl time.Duration, maxEntries int) *Loading[K, V] {
	if ttl <= 0 || maxEntries <= 0 {
		panic("cache: TTL and max entries must be positive")
	}
	return &Loading[K, V]{
		ttl: ttl, maxEntries: maxEntries, now: time.Now,
		entries: make(map[K]*list.Element), loads: make(map[K]*load[V]),
	}
}

// Get uses the caller's context for loading and waiting. If the leading caller
// is cancelled, other callers retry using their own contexts. Invalidation
// detaches pending loads, so their old values cannot refill or escape the cache.
func (c *Loading[K, V]) Get(ctx context.Context, key K, loader func(context.Context) (V, error)) (V, error) {
	for {
		if err := ctx.Err(); err != nil {
			var zero V
			return zero, err
		}
		c.mu.Lock()
		c.expire(c.now())
		if e := c.entries[key]; e != nil {
			value := e.Value.(entry[K, V]).value
			c.mu.Unlock()
			return value, nil
		}
		if pending := c.loads[key]; pending != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				var zero V
				return zero, ctx.Err()
			case <-pending.done:
				if pending.invalidated || errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded) {
					continue
				}
				return pending.value, pending.err
			}
		}
		pending := &load[V]{done: make(chan struct{})}
		c.loads[key] = pending
		c.mu.Unlock()

		value, err := c.runLoader(ctx, key, pending, loader)
		c.mu.Lock()
		if c.loads[key] != pending {
			// Invalidation closed this call and detached it before the DB read finished.
			c.mu.Unlock()
			continue
		}
		delete(c.loads, key)
		if err == nil {
			err = ctx.Err()
		}
		pending.value, pending.err = value, err
		if err == nil {
			now := c.now()
			c.expire(now)
			if len(c.entries) == c.maxEntries {
				c.remove(c.order.Front())
			}
			c.entries[key] = c.order.PushBack(entry[K, V]{key: key, value: value, expiresAt: now.Add(c.ttl)})
		}
		close(pending.done)
		c.mu.Unlock()
		return value, err
	}
}

// A panicking loader must not leave later requests waiting on an abandoned call.
// The defer also runs on runtime.Goexit; normal completion is published by Get.
func (c *Loading[K, V]) runLoader(ctx context.Context, key K, pending *load[V], loader func(context.Context) (V, error)) (V, error) {
	completed := false
	defer func() {
		if completed {
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.loads[key] == pending {
			delete(c.loads, key)
			pending.invalidated = true
			close(pending.done)
		}
	}()
	value, err := loader(ctx)
	completed = true
	return value, err
}

// Invalidate removes matching values and pending loads. The predicate runs under
// the cache lock and must not call back into the cache. It also matches keys
// whose first load has not completed yet.
func (c *Loading[K, V]) Invalidate(matches func(K) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, e := range c.entries {
		if matches(key) {
			c.remove(e)
		}
	}
	for key, pending := range c.loads {
		if matches(key) {
			delete(c.loads, key)
			pending.invalidated = true
			close(pending.done)
		}
	}
}

func (c *Loading[K, V]) expire(now time.Time) {
	for e := c.order.Front(); e != nil; e = c.order.Front() {
		if now.Before(e.Value.(entry[K, V]).expiresAt) {
			return
		}
		c.remove(e)
	}
}

func (c *Loading[K, V]) remove(e *list.Element) {
	delete(c.entries, e.Value.(entry[K, V]).key)
	c.order.Remove(e)
}
