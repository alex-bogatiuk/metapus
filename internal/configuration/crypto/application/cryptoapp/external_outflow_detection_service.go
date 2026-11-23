package cryptoapp

import (
	"context"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tx"
)

// DetectedExternalOutflow is the immutable identity resolved by the persistence
// adapter. Inserted distinguishes first detection from later observations of
// the same exact chain effect.
type DetectedExternalOutflow struct {
	OutflowID id.ID
	NetworkID id.ID
	WalletID  id.ID
	TokenID   *id.ID
	Inserted  bool
	Status    crypto.ExternalOutflowStatus
}

type ExpectedChainEffectMatcher interface {
	RouteExpectedTransfer(context.Context, crypto.BlockchainEvent) (bool, error)
}

type ObservedExternalOutflowRepository interface {
	UpsertExternalOutflow(context.Context, crypto.BlockchainEvent) (DetectedExternalOutflow, error)
}

type ExternalOutflowReconciliationRunRepository interface {
	CreateExternalOutflowRun(context.Context, DetectedExternalOutflow) (id.ID, error)
}

type ExternalOutflowIncidentRepository interface {
	UpsertExternalOutflowIncident(context.Context, id.ID, DetectedExternalOutflow, crypto.BlockchainEvent) (id.ID, error)
}

type ExternalOutflowObservationRepository interface {
	AppendExternalOutflowObservations(context.Context, id.ID, id.ID, DetectedExternalOutflow, crypto.BlockchainEvent) error
}

type ExternalOutflowActionRepository interface {
	AppendExternalOutflowDetected(context.Context, DetectedExternalOutflow, id.ID, crypto.BlockchainEvent) error
}

// ExternalOutflowDetectionService owns the complete atomic command boundary
// for an observed managed-wallet outflow. Persistence adapters only perform
// one operation each; they never decide workflow order or acquire holds.
type ExternalOutflowDetectionService struct {
	txm          tx.Manager
	matcher      ExpectedChainEffectMatcher
	outflows     ObservedExternalOutflowRepository
	runs         ExternalOutflowReconciliationRunRepository
	incidents    ExternalOutflowIncidentRepository
	observations ExternalOutflowObservationRepository
	actions      ExternalOutflowActionRepository
	holds        *OperationalHoldService
}

func NewExternalOutflowDetectionService(
	txm tx.Manager,
	matcher ExpectedChainEffectMatcher,
	outflows ObservedExternalOutflowRepository,
	runs ExternalOutflowReconciliationRunRepository,
	incidents ExternalOutflowIncidentRepository,
	observations ExternalOutflowObservationRepository,
	actions ExternalOutflowActionRepository,
	holds *OperationalHoldService,
) *ExternalOutflowDetectionService {
	if txm == nil || matcher == nil || outflows == nil || runs == nil || incidents == nil ||
		observations == nil || actions == nil || holds == nil {
		panic("ExternalOutflowDetectionService requires all transactional ports")
	}
	return &ExternalOutflowDetectionService{
		txm: txm, matcher: matcher, outflows: outflows, runs: runs,
		incidents: incidents, observations: observations, actions: actions, holds: holds,
	}
}

func (s *ExternalOutflowDetectionService) RecordExternalOutflow(ctx context.Context, event crypto.BlockchainEvent) error {
	event.TxHash = strings.TrimSpace(event.TxHash)
	event.FromAddress = strings.TrimSpace(event.FromAddress)
	event.ToAddress = strings.TrimSpace(event.ToAddress)
	event.TokenContract = strings.TrimSpace(event.TokenContract)
	event.BlockHash = strings.TrimSpace(event.BlockHash)
	event.EventKind = event.EffectiveEventKind()
	if id.IsNil(event.NetworkID) || event.TxHash == "" || !event.EventKind.Valid() || event.EventIndex < 0 ||
		event.FromAddress == "" || event.ToAddress == "" || !event.Amount.IsPositive() || event.BlockNumber < 0 ||
		event.Confirmations < 0 || event.RequiredConfs < 0 {
		return apperror.NewValidation("external outflow requires a complete chain identity and positive amount")
	}
	if event.Direction != crypto.TransferDirectionOutgoing {
		return apperror.NewValidation("external outflow must be an outgoing managed-wallet transfer")
	}

	return s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		known, err := s.matcher.RouteExpectedTransfer(txCtx, event)
		if err != nil || known {
			return err
		}

		detected, err := s.outflows.UpsertExternalOutflow(txCtx, event)
		if err != nil {
			return err
		}
		runID, err := s.runs.CreateExternalOutflowRun(txCtx, detected)
		if err != nil {
			return err
		}
		incidentID, err := s.incidents.UpsertExternalOutflowIncident(txCtx, runID, detected, event)
		if err != nil {
			return err
		}
		if err := s.observations.AppendExternalOutflowObservations(txCtx, runID, incidentID, detected, event); err != nil {
			return err
		}
		if !detected.Inserted {
			return nil
		}
		if err := s.actions.AppendExternalOutflowDetected(txCtx, detected, incidentID, event); err != nil {
			return err
		}

		ownerID := detected.OutflowID.String()
		if err := s.holds.Acquire(txCtx, AcquireOperationalHoldCommand{
			Scope: OperationalHoldScopeWallet, ScopeID: detected.WalletID, Reason: "external_outflow",
			OwnerType: "external_outflow", OwnerID: ownerID, HeldBy: "chain-watcher",
			Details: map[string]any{"incidentId": incidentID.String(), "txHash": event.TxHash},
		}); err != nil {
			return err
		}
		return s.holds.Acquire(txCtx, AcquireOperationalHoldCommand{
			Scope: OperationalHoldScopeNetwork, ScopeID: detected.NetworkID, Reason: "external_outflow",
			OwnerType: "external_outflow", OwnerID: ownerID, HeldBy: "chain-watcher",
			Details: map[string]any{
				"incidentId": incidentID.String(), "walletId": detected.WalletID.String(), "txHash": event.TxHash,
			},
		})
	})
}

var _ crypto.ExternalOutflowRecorder = (*ExternalOutflowDetectionService)(nil)
