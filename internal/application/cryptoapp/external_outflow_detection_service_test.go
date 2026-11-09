package cryptoapp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
)

type chainEffectMatcherStub struct {
	known bool
	calls int
}

func (s *chainEffectMatcherStub) MatchExpectedChainEffect(context.Context, crypto.BlockchainEvent) (bool, error) {
	s.calls++
	return s.known, nil
}

type outflowDetectionRepoStub struct {
	detected         DetectedExternalOutflow
	upsertCalls      int
	runCalls         int
	incidentCalls    int
	observationCalls int
	actionCalls      int
	runID            id.ID
	incidentID       id.ID
}

func (s *outflowDetectionRepoStub) UpsertExternalOutflow(context.Context, crypto.BlockchainEvent) (DetectedExternalOutflow, error) {
	s.upsertCalls++
	return s.detected, nil
}

func (s *outflowDetectionRepoStub) CreateExternalOutflowRun(context.Context, DetectedExternalOutflow) (id.ID, error) {
	s.runCalls++
	return s.runID, nil
}

func (s *outflowDetectionRepoStub) UpsertExternalOutflowIncident(context.Context, id.ID, DetectedExternalOutflow, crypto.BlockchainEvent) (id.ID, error) {
	s.incidentCalls++
	return s.incidentID, nil
}

func (s *outflowDetectionRepoStub) AppendExternalOutflowObservations(context.Context, id.ID, id.ID, DetectedExternalOutflow, crypto.BlockchainEvent) error {
	s.observationCalls++
	return nil
}

func (s *outflowDetectionRepoStub) AppendExternalOutflowDetected(context.Context, DetectedExternalOutflow, id.ID, crypto.BlockchainEvent) error {
	s.actionCalls++
	return nil
}

type detectionHoldRepoStub struct {
	acquired []AcquireOperationalHoldCommand
}

func (s *detectionHoldRepoStub) AcquireOperationalHold(_ context.Context, command AcquireOperationalHoldCommand) error {
	s.acquired = append(s.acquired, command)
	return nil
}

func (s *detectionHoldRepoStub) ResolveOperationalHolds(context.Context, string, string, string, string) (int64, error) {
	return 0, nil
}

func validExternalOutflowEvent() crypto.BlockchainEvent {
	return crypto.BlockchainEvent{
		NetworkID: id.New(), TxHash: "tx-external-1", EventKind: crypto.EventKindContractLog,
		EventIndex: 3, FromAddress: "TManaged", ToAddress: "TDestination",
		TokenContract: "TContract", Amount: types.MustCryptoAmount("42"), BlockNumber: 10,
		Confirmations: 1, RequiredConfs: 19, Direction: crypto.TransferDirectionOutgoing,
	}
}

func newDetectionServiceForTest(matcher *chainEffectMatcherStub, repo *outflowDetectionRepoStub, holdsRepo *detectionHoldRepoStub) (*ExternalOutflowDetectionService, *immediateTxManager) {
	txm := &immediateTxManager{}
	holds := NewOperationalHoldService(&immediateTxManager{}, holdsRepo)
	return NewExternalOutflowDetectionService(txm, matcher, repo, repo, repo, repo, repo, holds), txm
}

func TestExternalOutflowDetectionStopsAtExactKnownEffect(t *testing.T) {
	matcher := &chainEffectMatcherStub{known: true}
	repo := &outflowDetectionRepoStub{}
	holdsRepo := &detectionHoldRepoStub{}
	service, txm := newDetectionServiceForTest(matcher, repo, holdsRepo)

	require.NoError(t, service.RecordExternalOutflow(context.Background(), validExternalOutflowEvent()))
	require.Equal(t, 1, txm.calls)
	require.Equal(t, 1, matcher.calls)
	require.Zero(t, repo.upsertCalls)
	require.Empty(t, holdsRepo.acquired)
}

func TestExternalOutflowDetectionCreatesEvidenceAndOwnedHoldsOnce(t *testing.T) {
	event := validExternalOutflowEvent()
	repo := &outflowDetectionRepoStub{
		detected: DetectedExternalOutflow{
			OutflowID: id.New(), NetworkID: event.NetworkID, WalletID: id.New(), TokenID: ptrID(id.New()),
			Inserted: true, Status: crypto.ExternalOutflowPendingReview,
		},
		runID: id.New(), incidentID: id.New(),
	}
	holdsRepo := &detectionHoldRepoStub{}
	service, txm := newDetectionServiceForTest(&chainEffectMatcherStub{}, repo, holdsRepo)

	require.NoError(t, service.RecordExternalOutflow(context.Background(), event))
	require.Equal(t, 1, txm.calls)
	require.Equal(t, 1, repo.upsertCalls)
	require.Equal(t, 1, repo.runCalls)
	require.Equal(t, 1, repo.incidentCalls)
	require.Equal(t, 1, repo.observationCalls)
	require.Equal(t, 1, repo.actionCalls)
	require.Len(t, holdsRepo.acquired, 2)
	require.Equal(t, OperationalHoldScopeWallet, holdsRepo.acquired[0].Scope)
	require.Equal(t, OperationalHoldScopeNetwork, holdsRepo.acquired[1].Scope)
	for _, hold := range holdsRepo.acquired {
		require.Equal(t, "external_outflow", hold.OwnerType)
		require.Equal(t, repo.detected.OutflowID.String(), hold.OwnerID)
	}
}

func TestRepeatedExternalOutflowAppendsEvidenceWithoutNewActionOrHolds(t *testing.T) {
	event := validExternalOutflowEvent()
	repo := &outflowDetectionRepoStub{
		detected: DetectedExternalOutflow{
			OutflowID: id.New(), NetworkID: event.NetworkID, WalletID: id.New(), TokenID: ptrID(id.New()),
			Inserted: false, Status: crypto.ExternalOutflowPendingReview,
		},
		runID: id.New(), incidentID: id.New(),
	}
	holdsRepo := &detectionHoldRepoStub{}
	service, _ := newDetectionServiceForTest(&chainEffectMatcherStub{}, repo, holdsRepo)

	require.NoError(t, service.RecordExternalOutflow(context.Background(), event))
	require.Equal(t, 1, repo.observationCalls)
	require.Zero(t, repo.actionCalls)
	require.Empty(t, holdsRepo.acquired)
}

func TestExternalOutflowDetectionRejectsIncompleteIdentityBeforeTransaction(t *testing.T) {
	repo := &outflowDetectionRepoStub{}
	service, txm := newDetectionServiceForTest(&chainEffectMatcherStub{}, repo, &detectionHoldRepoStub{})
	event := validExternalOutflowEvent()
	event.EventIndex = -1

	require.Error(t, service.RecordExternalOutflow(context.Background(), event))
	require.Zero(t, txm.calls)
	require.Zero(t, repo.upsertCalls)
}

func ptrID(value id.ID) *id.ID { return &value }
