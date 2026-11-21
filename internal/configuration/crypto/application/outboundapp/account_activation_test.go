package outboundapp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
)

type heldActivationStore struct {
	ActivationRepository
	held, external int
}

func (s *heldActivationStore) PrepareAccountActivationState(context.Context, AccountActivationTask) error {
	return nil
}
func (s *heldActivationStore) ApplyAccountActivationClaim(_ context.Context, _ AccountActivationTask, update ActivationClaimUpdate) error {
	if update.Action == ActivationClaimHeld {
		s.held++
	}
	if update.Action == ActivationClaimExternallyActive {
		s.external++
	}
	return nil
}

func (s *heldActivationStore) MarkSigningAuthorizationHeld(context.Context, string, string) error {
	return nil
}
func (s *heldActivationStore) MarkAccountActivationHeld(context.Context, id.ID, string) error {
	s.held++
	return nil
}
func (s *heldActivationStore) MarkAccountActivationExternallyActive(context.Context, id.ID, id.ID) error {
	s.external++
	return nil
}

func TestActiveAccountWithUnresolvedSignedEffectKeepsReservationHeld(t *testing.T) {
	store := &heldActivationStore{}
	service := NewAccountActivationExecutor(ActivationConfig{Repository: store, Transactions: &workflowTx{}})
	err := service.handleAlreadyActiveAccount(t.Context(), AccountActivationTask{ActivationID: id.New()}, true, SigningAuthorizationRecord{SignedTx: &crypto.SignedTxEnvelope{TxHash: "unresolved-effect"}})
	require.True(t, IsHeldExecutionError(err))
	require.Equal(t, 1, store.held)
	require.Zero(t, store.external)
	// Fee release and finality posting are not configured: seeing an active
	// account alone cannot prove which signed transaction caused the effect.
}
