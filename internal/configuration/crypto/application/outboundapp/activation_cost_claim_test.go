package outboundapp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
)

type costRepairStoreStub struct {
	ActivationRepository
	prepareErr     error
	prepared, held []ActivationCostRepairTask
}

func (s *costRepairStoreStub) PrepareActivationCostRepair(_ context.Context, task ActivationCostRepairTask) error {
	s.prepared = append(s.prepared, task)
	return s.prepareErr
}
func (s *costRepairStoreStub) MarkActivationCostRepairHeld(_ context.Context, task ActivationCostRepairTask, _ string) error {
	s.held = append(s.held, task)
	return nil
}

type costRepairFeeStub struct {
	ActivationFees
	reservation crypto.FeeReservation
}

func (s costRepairFeeStub) Get(context.Context, string, id.ID, crypto.SigningPurpose, int) (crypto.FeeReservation, bool, error) {
	return s.reservation, true, nil
}

type costRepairOperationStub struct {
	ActivationOperations
	tasks []ActivationCostRepairTask
	err   error
}

func (s *costRepairOperationStub) FinalizeActivationRepair(_ context.Context, task ActivationCostRepairTask, _ id.ID, _ crypto.ChainReceipt) (*crypto_network_operation.CryptoNetworkOperation, error) {
	s.tasks = append(s.tasks, task)
	return nil, s.err
}

func TestActivationCostRepairRejectsExpiredQueueClaimBeforeRPC(t *testing.T) {
	stale := apperror.NewConflict("stale cost claim")
	store := &costRepairStoreStub{prepareErr: stale}
	service := NewAccountActivationExecutor(ActivationConfig{Repository: store})
	task := ActivationCostRepairTask{ActivationID: id.New(), CostClaimToken: id.New()}
	require.ErrorIs(t, service.RepairCost(t.Context(), task), stale)
	require.Equal(t, []ActivationCostRepairTask{task}, store.prepared)
	require.Empty(t, store.held)
}

func TestActivationCostRepairUsesGuardedCompletionAfterRPC(t *testing.T) {
	task := ActivationCostRepairTask{ActivationID: id.New(), NetworkID: id.New(), CostClaimToken: id.New(), TxHash: "repair-hash"}
	driver, drivers := newWorkflowDriver(t)
	task.NetworkID = driver.network.ID
	driver.receipt = crypto.ChainReceipt{NetworkID: task.NetworkID, TxHash: task.TxHash, Status: crypto.ReceiptSucceeded, Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalityFinalized}
	store := &costRepairStoreStub{}
	operations := &costRepairOperationStub{err: apperror.NewConflict("claim changed during RPC")}
	service := NewAccountActivationExecutor(ActivationConfig{Repository: store, Drivers: drivers, Fees: costRepairFeeStub{reservation: crypto.FeeReservation{ID: id.New()}}, Operations: operations})
	require.ErrorIs(t, service.RepairCost(t.Context(), task), operations.err)
	require.Equal(t, []ActivationCostRepairTask{task}, operations.tasks)
	require.Equal(t, []ActivationCostRepairTask{task}, store.held)
	// The embedded unguarded FinalizeActivation is deliberately unconfigured.
}
