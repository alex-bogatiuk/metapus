package crypto_worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
)

func TestNetworkSetSignatureFollowsWatchedNetworksAndTheirTokens(t *testing.T) {
	nile, sepolia := id.New(), id.New()
	usdt, eth, pyusd := id.New(), id.New(), id.New()
	both := []networkInfo{
		{ref: crypto.NetworkRef{ID: nile}, assets: []crypto.AssetRef{{TokenID: usdt}}},
		{ref: crypto.NetworkRef{ID: sepolia}, assets: []crypto.AssetRef{{TokenID: eth}, {TokenID: pyusd}}},
	}
	reordered := []networkInfo{
		{ref: crypto.NetworkRef{ID: sepolia}, assets: []crypto.AssetRef{{TokenID: pyusd}, {TokenID: eth}}},
		both[0],
	}
	require.Equal(t, networkSetSignature(both), networkSetSignature(reordered), "loading order is not a change")
	require.NotEqual(t, networkSetSignature(both), networkSetSignature(both[:1]), "a network that stops being watched is a change")
	require.NotEqual(t, networkSetSignature(both), networkSetSignature([]networkInfo{both[0], {ref: crypto.NetworkRef{ID: sepolia}, assets: []crypto.AssetRef{{TokenID: eth}}}}),
		"a token whose watcher stops is a change")
}

func TestWatchNetworkSetStopsTheAttemptOnlyOnAChange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var reads atomic.Int32
	load := func(context.Context) (string, error) {
		switch reads.Add(1) {
		case 1:
			return "", errors.New("catalog unavailable")
		case 2:
			return "initial", nil
		default:
			return "changed", nil
		}
	}
	var changed atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchNetworkSet(ctx, time.Millisecond, "initial", load, &changed, cancel)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a changed network set did not stop the attempt")
	}
	require.True(t, changed.Load())
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.GreaterOrEqual(t, reads.Load(), int32(3), "a failed read and an unchanged set keep the attempt running")
}
