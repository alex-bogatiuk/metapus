package crypto

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/apperror"
)

type expectedRouterStub struct {
	calls   int
	handled bool
	err     error
}

func (s *expectedRouterStub) RouteExpectedTransfer(context.Context, BlockchainEvent) (bool, error) {
	s.calls++
	return s.handled, s.err
}

type expectedOutflowStub struct{ calls int }

func (s *expectedOutflowStub) RecordExternalOutflow(context.Context, BlockchainEvent) error {
	s.calls++
	return nil
}
func TestExpectedTransferRoutingPrecedesDirectionAndExternalMatching(t *testing.T) {
	for _, direction := range []TransferDirection{TransferDirectionIncoming, TransferDirectionInternal, TransferDirectionOutgoing, TransferDirectionUnknown} {
		router := &expectedRouterStub{handled: true}
		outflow := &expectedOutflowStub{}
		p := NewEventProcessor(EventProcessorConfig{TxManager: &noopTxManager{}, ExpectedTransfers: router, OutflowRecorder: outflow})
		require.NoError(t, p.ProcessEvent(t.Context(), BlockchainEvent{Direction: direction}))
		require.Equal(t, 1, router.calls)
		require.Zero(t, outflow.calls)
	}
	router := &expectedRouterStub{err: apperror.NewConflict("expected route conflict")}
	outflow := &expectedOutflowStub{}
	p := NewEventProcessor(EventProcessorConfig{TxManager: &noopTxManager{}, ExpectedTransfers: router, OutflowRecorder: outflow})
	require.Error(t, p.ProcessEvent(t.Context(), BlockchainEvent{Direction: TransferDirectionOutgoing}))
	require.Zero(t, outflow.calls)
	router.err = nil
	require.NoError(t, p.ProcessEvent(t.Context(), BlockchainEvent{Direction: TransferDirectionOutgoing}))
	require.Equal(t, 1, outflow.calls)
	require.NoError(t, p.ProcessEvent(t.Context(), BlockchainEvent{Direction: TransferDirectionIncoming, ObservationMode: "shadow"}))
	require.Equal(t, 2, router.calls)
}
