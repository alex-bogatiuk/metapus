package rpcpolicy

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Runtime owns generations of an RPC client pool. A logical operation pins one
// generation; replacement never closes the clients of an in-flight operation.
type Runtime[T any, C any] struct {
	mu         sync.Mutex
	current    *generation[T]
	load       func(context.Context) (C, string, error)
	build      func(context.Context, C) (*T, error)
	close      func(*T)
	version    string
	nextCheck  time.Time
	lastRead   time.Time
	blocked    error
	refreshing bool
	closed     bool
	Interval   time.Duration
}
type generation[T any] struct {
	value      *T
	version    string
	references int
	retired    bool
}

func NewRuntime[T any, C any](initial *T, version string, load func(context.Context) (C, string, error), build func(context.Context, C) (*T, error), closeFn func(*T)) *Runtime[T, C] {
	return &Runtime[T, C]{current: &generation[T]{value: initial, version: version}, version: version, load: load, build: build, close: closeFn, Interval: 10 * time.Second, lastRead: time.Now()}
}
func (r *Runtime[T, C]) Acquire(ctx context.Context) (*T, func(), error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, func() {}, errors.New("RPC runtime closed")
	}
	refresh := r.load != nil && !r.refreshing && !time.Now().Before(r.nextCheck)
	if refresh {
		r.refreshing = true
		r.nextCheck = time.Now().Add(r.Interval)
	}
	r.mu.Unlock()
	if refresh {
		loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		cfg, version, err := r.load(loadCtx)
		cancel()
		r.mu.Lock()
		same := version == r.version
		if err == nil {
			r.lastRead = time.Now()
		}
		r.mu.Unlock()
		var replacement *T
		if err == nil && !same {
			replacement, err = r.build(ctx, cfg)
		}
		r.mu.Lock()
		r.refreshing = false
		if r.closed {
			r.mu.Unlock()
			if replacement != nil {
				r.close(replacement)
			}
			return nil, func() {}, errors.New("RPC runtime closed")
		}
		if err != nil {
			// A known but invalid new topology must not silently use the old keys.
			if version != "" && !same || time.Since(r.lastRead) > 60*time.Second {
				r.blocked = errors.New("RPC configuration unavailable")
			}
		} else {
			r.blocked = nil
			if replacement != nil {
				old := r.current
				old.retired = true
				r.current = &generation[T]{value: replacement, version: version}
				r.version = version
				if old.references == 0 {
					r.close(old.value)
				}
			}
		}
		r.mu.Unlock()
	}
	return r.acquireCurrent()
}
func (r *Runtime[T, C]) Peek() (*T, func(), error) { return r.acquireCurrent() }
func (r *Runtime[T, C]) acquireCurrent() (*T, func(), error) {
	value, _, release, err := r.PeekSnapshot()
	return value, release, err
}

// PeekSnapshot pins the generation and its installed version together.
func (r *Runtime[T, C]) PeekSnapshot() (*T, string, func(), error) {
	r.mu.Lock()
	if r.load != nil && time.Since(r.lastRead) > 60*time.Second {
		r.blocked = errors.New("RPC configuration unavailable")
	}
	if r.closed || r.blocked != nil {
		err := r.blocked
		if err == nil {
			err = errors.New("RPC runtime closed")
		}
		r.mu.Unlock()
		return nil, "", func() {}, err
	}
	g := r.current
	g.references++
	r.mu.Unlock()
	var once sync.Once
	return g.value, g.version, func() {
		once.Do(func() {
			r.mu.Lock()
			g.references--
			closeNow := g.retired && g.references == 0
			r.mu.Unlock()
			if closeNow {
				r.close(g.value)
			}
		})
	}, nil
}
func (r *Runtime[T, C]) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	g := r.current
	g.retired = true
	closeNow := g.references == 0
	r.mu.Unlock()
	if closeNow {
		r.close(g.value)
	}
}
