package crypto_worker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// errNetworkSetChanged ends a processor attempt so that the supervisor starts
// the next one with exactly the watched networks and their active tokens.
var errNetworkSetChanged = errors.New("watched blockchain networks changed; restarting crypto processing")

const networkSetCheckPeriod = 15 * time.Second

// networkSetSignature identifies the watched networks and the tokens their
// watchers follow; a network that stops being watched leaves the signature.
func networkSetSignature(networks []networkInfo) string {
	parts := make([]string, 0, len(networks))
	for _, network := range networks {
		tokens := make([]string, 0, len(network.assets))
		for _, asset := range network.assets {
			tokens = append(tokens, asset.TokenID.String())
		}
		slices.Sort(tokens)
		parts = append(parts, network.ref.ID.String()+":"+strings.Join(tokens, ","))
	}
	slices.Sort(parts)
	return strings.Join(parts, ";")
}

func (p *CryptoProcessor) loadNetworkSetSignature(ctx context.Context) (string, error) {
	networks, err := p.loadNetworks(ctx)
	if err != nil {
		return "", err
	}
	return networkSetSignature(networks), nil
}

// watchNetworkSet stops the attempt when the watched set differs from the one
// it started with. A failed read is retried on the next tick: an unreadable
// catalog is not a reason to stop reading the networks already running.
func watchNetworkSet(ctx context.Context, period time.Duration, initial string, load func(context.Context) (string, error), changed *atomic.Bool, stop context.CancelFunc) {
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current, err := load(ctx)
			if err != nil || current == initial {
				continue
			}
			changed.Store(true)
			stop()
			return
		}
	}
}
