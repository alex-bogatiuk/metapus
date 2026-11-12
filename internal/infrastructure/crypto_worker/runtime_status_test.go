package crypto_worker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/domain/crypto"
)

func TestRuntimeIngestionStatusKeepsEvaluatedStateWithoutLeaseOwner(t *testing.T) {
	report := IngestionNetworkReport{State: "catching_up", RPCErrorCode: "", Streams: []IngestionStreamReport{
		{
			WatcherKey: "tron:solidified", State: "catching_up", Reason: "backlog",
			Head:       &crypto.IngestionHead{Height: 120},
			Checkpoint: &crypto.WatcherState{LastBlock: 100, Fingerprint: "00ab", LeaseOwner: "worker-1", FencingToken: 7},
			LagBlocks:  20, LagSeconds: 60, ProgressAgeSeconds: 3, LeaseActive: true,
		},
		{WatcherKey: "tron:latest", Optional: true, State: "degraded", Reason: "ingestion RPC coverage unavailable", LagBlocks: -1, LagSeconds: -1, ProgressAgeSeconds: -1},
	}}

	status := runtimeIngestionStatus(report)

	require.Equal(t, "catching_up", status.State)
	require.Len(t, status.Streams, 2)
	primary := status.Streams[0]
	require.Equal(t, int64(100), *primary.LastBlock)
	require.Equal(t, int64(120), *primary.HeadHeight)
	require.Equal(t, "00ab", primary.BlockHash)
	require.Equal(t, "backlog", primary.Reason)
	require.True(t, primary.LeaseActive)
	missing := status.Streams[1]
	require.Nil(t, missing.LastBlock)
	require.Nil(t, missing.HeadHeight)
	require.True(t, missing.Optional)
	require.Equal(t, int64(-1), missing.LagBlocks)
}
