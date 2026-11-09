package rpcpolicy

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRuntimeDrainsRetiredGeneration(t *testing.T) {
	first, next := 1, 2
	var version atomic.Int64
	version.Store(1)
	var closed atomic.Int64
	r := NewRuntime(&first, "1", func(context.Context) (int, string, error) {
		if version.Load() == 1 {
			return 1, "1", nil
		}
		return next, "2", nil
	}, func(_ context.Context, c int) (*int, error) { return &c, nil }, func(v *int) { closed.Add(int64(*v)) })
	r.Interval = 0
	a, release, err := r.Acquire(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, *a)
	version.Store(2)
	b, releaseB, err := r.Acquire(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, *b)
	require.Zero(t, closed.Load())
	release()
	release()
	require.EqualValues(t, 1, closed.Load())
	r.Close()
	require.EqualValues(t, 1, closed.Load())
	releaseB()
	require.EqualValues(t, 3, closed.Load())
	r.Close()
	_, _, err = r.Acquire(t.Context())
	require.Error(t, err)
}
func TestRuntimeUnchangedConfigDoesNotRebuildAndInvalidReplacementBlocks(t *testing.T) {
	first := 1
	version := "1"
	builds := 0
	r := NewRuntime(&first, "1", func(context.Context) (int, string, error) { return 2, version, nil }, func(context.Context, int) (*int, error) {
		builds++
		return nil, errors.New("invalid new configuration")
	}, func(*int) {})
	r.Interval = 0
	defer r.Close()
	_, release, err := r.Acquire(t.Context())
	require.NoError(t, err)
	release()
	require.Zero(t, builds)
	version = "2"
	_, _, err = r.Acquire(t.Context())
	require.Error(t, err)
	require.Equal(t, 1, builds)
	_, _, err = r.Peek()
	require.Error(t, err)
	version = "1"
	_, release, err = r.Acquire(t.Context())
	require.NoError(t, err)
	release()
}

func TestRuntimeCacheExpiresDuringDatabaseOutage(t *testing.T) {
	first := 1
	r := NewRuntime(&first, "1", func(context.Context) (int, string, error) { return 0, "", errors.New("database unavailable") }, func(context.Context, int) (*int, error) { panic("unexpected build") }, func(*int) {})
	r.Interval = 0
	defer r.Close()
	_, release, err := r.Acquire(t.Context())
	require.NoError(t, err)
	release()
	r.lastRead = time.Now().Add(-61 * time.Second)
	_, _, err = r.Acquire(t.Context())
	require.Error(t, err)
}
