package crypto_worker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
)

func TestIngestionNetworksDoNotSpendEachOthersDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	secondStarted := make(chan struct{})
	networks := []networkInfo{{ref: crypto.NetworkRef{Code: "NILE"}}, {ref: crypto.NetworkRef{Code: "SEPOLIA"}}}
	reports, err := readIngestionNetworks(ctx, networks, func(ctx context.Context, network networkInfo) (IngestionNetworkReport, error) {
		if network.ref.Code == "NILE" {
			select {
			case <-secondStarted:
			case <-ctx.Done():
				return IngestionNetworkReport{}, ctx.Err()
			}
		} else {
			close(secondStarted)
		}
		return IngestionNetworkReport{NetworkCode: network.ref.Code, State: "ready"}, nil
	})
	require.NoError(t, err, "the first network cannot wait for the second in a sequential collector")
	require.Equal(t, "NILE", reports[0].NetworkCode)
	require.Equal(t, "SEPOLIA", reports[1].NetworkCode, "concurrent results retain deterministic network order")
}

type parallelIngestionReader struct {
	crypto.NetworkDriver
	latestStarted   chan struct{}
	primaryDeadline time.Time
	streamDeadline  time.Time
}

func (r *parallelIngestionReader) ReadIngestionHead(ctx context.Context, _ []crypto.AssetRef) (crypto.IngestionHead, error) {
	r.primaryDeadline, _ = ctx.Deadline()
	select {
	case <-r.latestStarted:
		return crypto.IngestionHead{Height: 100}, nil
	case <-ctx.Done():
		return crypto.IngestionHead{}, ctx.Err()
	}
}

func (r *parallelIngestionReader) ReadStreamHead(ctx context.Context, _ string, _ []crypto.AssetRef) (crypto.IngestionHead, error) {
	r.streamDeadline, _ = ctx.Deadline()
	close(r.latestStarted)
	return crypto.IngestionHead{Height: 120}, nil
}

func (*parallelIngestionReader) StreamRequired(key string) bool { return key == "tron:solidified" }

func TestIngestionHeadReadsPreserveAdapterBudgetAndRunIndependently(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	reader := &parallelIngestionReader{latestStarted: make(chan struct{})}
	head, streams, err := readIngestionHeads(ctx, reader, reader, nil, []string{"tron:solidified", "tron:latest"})
	require.NoError(t, err)
	require.Equal(t, int64(100), head.Height)
	require.Equal(t, int64(120), streams["tron:latest"].head.Height)
	deadline, _ := ctx.Deadline()
	require.Equal(t, deadline, reader.primaryDeadline, "no hard-coded 12 s deadline may truncate the adapter")
	require.Equal(t, deadline, reader.streamDeadline)
}
