package outboundapp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
)

type failedAttemptStore struct {
	ActivationRepository
	exposed bool
	claims  []ActivationClaimUpdate
}

func (s *failedAttemptStore) PrepareAccountActivationState(context.Context, AccountActivationTask) error {
	return nil
}
func (s *failedAttemptStore) HasAccountActivationSigningExposure(context.Context, AccountActivationTask) (bool, error) {
	return s.exposed, nil
}
func (s *failedAttemptStore) ApplyAccountActivationClaim(_ context.Context, _ AccountActivationTask, update ActivationClaimUpdate) error {
	s.claims = append(s.claims, update)
	return nil
}

type failedAttemptFees struct {
	ActivationFees
	released, held int
}

func (s *failedAttemptFees) ReleaseUnbroadcast(context.Context, id.ID, string) error {
	s.released++
	return nil
}
func (s *failedAttemptFees) Hold(context.Context, id.ID, string) error {
	s.held++
	return nil
}

func TestFailedUnsignedSigningAttemptPredicate(t *testing.T) {
	require.True(t, FailedUnsignedSigningAttempt(SigningAuthorizationRecord{Status: "failed"}))
	require.False(t, FailedUnsignedSigningAttempt(SigningAuthorizationRecord{Status: "held"}), "held attempts keep their existing recovery")
	require.False(t, FailedUnsignedSigningAttempt(SigningAuthorizationRecord{Status: "failed", TxID: "known"}))
	require.False(t, FailedUnsignedSigningAttempt(SigningAuthorizationRecord{Status: "failed", SignedTx: &crypto.SignedTxEnvelope{}}))
}

// The stand activation that failed at "authorize" kept its released
// reservation and occupied the only activation slot forever.
func TestFailedUnsignedActivationAttemptIsRetiredToFreeTheSlot(t *testing.T) {
	for _, status := range []crypto.FeeReservationStatus{crypto.FeeReservationReleased, crypto.FeeReservationReserved} {
		t.Run(string(status), func(t *testing.T) {
			store, fees := &failedAttemptStore{}, &failedAttemptFees{}
			executor := NewAccountActivationExecutor(ActivationConfig{Repository: store, Fees: fees, Transactions: &workflowTx{}})
			require.NoError(t, executor.retireFailedUnsignedAttempt(t.Context(), AccountActivationTask{ActivationID: id.New()}, crypto.FeeReservation{ID: id.New(), Status: status}))
			require.Len(t, store.claims, 1)
			require.Equal(t, ActivationClaimFailed, store.claims[0].Action)
			if status == crypto.FeeReservationReleased {
				require.Zero(t, fees.released, "an already released reservation is not released again")
			} else {
				require.Equal(t, 1, fees.released)
			}
		})
	}
}

func TestFailedActivationAttemptWithSigningExposureStaysHeld(t *testing.T) {
	store, fees := &failedAttemptStore{exposed: true}, &failedAttemptFees{}
	executor := NewAccountActivationExecutor(ActivationConfig{Repository: store, Fees: fees, Transactions: &workflowTx{}})
	err := executor.retireFailedUnsignedAttempt(t.Context(), AccountActivationTask{ActivationID: id.New()}, crypto.FeeReservation{ID: id.New(), Status: crypto.FeeReservationReserved})
	require.Error(t, err)
	require.Equal(t, ActivationClaimHeld, store.claims[0].Action)
	require.Equal(t, 1, fees.held)
	require.Zero(t, fees.released)
}

type externallyActiveStore struct {
	failedAttemptStore
	markedFailed int
}

func (s *externallyActiveStore) MarkSigningAuthorizationFailed(context.Context, string, string) error {
	s.markedFailed++
	return nil
}

type externallyActiveFees struct {
	failedAttemptFees
}

func (s *externallyActiveFees) Get(context.Context, string, id.ID, crypto.SigningPurpose, int) (crypto.FeeReservation, bool, error) {
	return crypto.FeeReservation{ID: id.New(), Status: crypto.FeeReservationReleased}, true, nil
}

// MarkSigningAuthorizationFailed refuses an already failed authorization, so
// an externally activated account must not repeat it.
func TestExternallyActiveAccountDoesNotRefailAFailedAuthorization(t *testing.T) {
	store, fees := &externallyActiveStore{}, &externallyActiveFees{}
	executor := NewAccountActivationExecutor(ActivationConfig{Repository: store, Fees: fees, Transactions: &workflowTx{}})
	require.NoError(t, executor.handleAlreadyActiveAccount(t.Context(), AccountActivationTask{ActivationID: id.New()}, true, SigningAuthorizationRecord{Status: "failed"}))
	require.Zero(t, store.markedFailed)
	require.Equal(t, ActivationClaimExternallyActive, store.claims[0].Action)
	require.NoError(t, executor.handleAlreadyActiveAccount(t.Context(), AccountActivationTask{ActivationID: id.New()}, true, SigningAuthorizationRecord{Status: "held"}))
	require.Equal(t, 1, store.markedFailed)
}
