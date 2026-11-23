package tron

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type pausedCaptureRepo struct{ captureRepoStub }

func (*pausedCaptureRepo) Cutover(context.Context, id.ID) (crypto.IngestionCutover, error) {
	return crypto.IngestionCutover{Mode: "shadow", ShadowPaused: true}, nil
}

func TestPausedShadowDoesNotAcquireLeaseOrCallRPC(t *testing.T) {
	// Deliberately no RPC pool/state repository: reaching either would panic.
	w := &blockWatcher{shadow: true, driver: &Driver{blockRepo: &pausedCaptureRepo{}}}
	require.ErrorIs(t, w.run(t.Context(), nil), crypto.ErrShadowPaused)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx, nil) }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("paused watcher failed to drain on cancellation")
	}
}
