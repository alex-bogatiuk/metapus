//go:build integration

package crypto_repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
)

func TestRuntimeStatusCarriesIngestionStreamsToTheAdminState(t *testing.T) {
	f := newNetworkWatchFixture(t)
	block, head := int64(1200), int64(1203)
	status := crypto.NetworkRuntimeStatus{
		NetworkID: f.network, ObservedAt: time.Now().UTC(), WorkerID: "worker-a", ResourceProviderState: "unconfigured",
		Ingestion: &crypto.NetworkIngestionStatus{State: "catching_up", Streams: []crypto.IngestionStreamStatus{{
			WatcherKey: "native_eth", State: "catching_up", Reason: "backlog", LastBlock: &block, BlockHash: "0xabc",
			HeadHeight: &head, LagBlocks: 3, LagSeconds: 36, ProgressAgeSeconds: 4, LeaseActive: true,
		}}},
	}
	repo := NewWorkerRuntimeRepo()
	require.NoError(t, repo.PersistNetworkRuntimeStatus(f.ctx, status))

	state, err := NewNetworkPolicyRepo().GetNetworkPolicyState(f.ctx, f.network)
	require.NoError(t, err)
	require.Equal(t, status.Ingestion, state.Runtime.Ingestion)

	// A worker that could not evaluate ingestion must not leave the previous
	// evaluation on the page as if it were current.
	status.Ingestion = nil
	require.NoError(t, repo.PersistNetworkRuntimeStatus(f.ctx, status))
	state, err = NewNetworkPolicyRepo().GetNetworkPolicyState(f.ctx, f.network)
	require.NoError(t, err)
	require.Nil(t, state.Runtime.Ingestion)
}
