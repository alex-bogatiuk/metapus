package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	platformtelemetry "metapus/internal/platform/telemetry"
)

const _observationPollPeriod = time.Second
const _observationLease = 5 * time.Minute

func (p *CryptoProcessor) captureChainObservation(ctx context.Context, event crypto.BlockchainEvent) error {
	ctx, span := otel.Tracer("metapus/crypto/observation").Start(ctx, "chain.observation.capture",
		oteltrace.WithSpanKind(oteltrace.SpanKindProducer), oteltrace.WithAttributes(
			attribute.String("chain.network_id", event.NetworkID.String()),
			attribute.String("chain.event_kind", string(event.EffectiveEventKind())),
		))
	defer span.End()
	if err := crypto_repo.NewChainObservationRepo().Capture(ctx, event); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "observation capture failed")
		return err
	}
	return nil
}

func (p *CryptoProcessor) runChainObservationLoop(ctx context.Context) {
	ticker := time.NewTicker(_observationPollPeriod)
	defer ticker.Stop()
	for {
		if err := p.processChainObservations(ctx); err != nil && ctx.Err() == nil {
			p.log.Errorw("chain observation processing failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *CryptoProcessor) processChainObservations(ctx context.Context) error {
	repo := crypto_repo.NewChainObservationRepo()
	var claimed []crypto_repo.ChainObservation
	err := (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
		var claimErr error
		claimed, claimErr = repo.Claim(txCtx, p.workerID, 50, _observationLease)
		return claimErr
	})
	if err != nil {
		return err
	}
	for _, observation := range claimed {
		if err := p.processClaimedChainObservation(ctx, repo, observation); err != nil {
			return err
		}
	}
	return nil
}

func (p *CryptoProcessor) processClaimedChainObservation(ctx context.Context, repo *crypto_repo.ChainObservationRepo, observation crypto_repo.ChainObservation) error {
	workCtx := platformtelemetry.ContinueTrace(ctx, observation.Traceparent)
	workCtx = platformtelemetry.WithBusinessContext(workCtx, observation.CorrelationID, observation.CausationID)
	workCtx, span := otel.Tracer("metapus/crypto/observation").Start(workCtx, "chain.observation.process",
		oteltrace.WithSpanKind(oteltrace.SpanKindConsumer), oteltrace.WithAttributes(
			attribute.String("chain.observation_id", observation.ID.String()),
			attribute.String("chain.network_id", observation.NetworkID.String()),
			attribute.String("business.correlation_id", observation.CorrelationID),
			attribute.String("business.causation_id", observation.CausationID),
		))
	defer span.End()
	var event crypto.BlockchainEvent
	processErr := json.Unmarshal(observation.Payload, &event)
	if processErr == nil && event.VerificationID == nil && (event.Finality == crypto.FinalitySolidified || event.EventType == crypto.EventTypeReorg) && p.drivers != nil {
		driver, err := p.drivers.Resolve(event.NetworkID)
		if err != nil {
			processErr = err
		} else if verifier, ok := driver.(crypto.TransferVerifier); ok && verifier.VerificationRequired() {
			if !event.Amount.IsPositive() || event.FromAddress == "" || event.ToAddress == "" {
				payment, err := document_repo.NewCryptoPaymentRepo().FindByChainEvent(workCtx, event.NetworkID, event.TxHash, string(event.EffectiveEventKind()), event.EventIndex)
				if err != nil {
					processErr = err
				} else if payment != nil {
					event, processErr = crypto_repo.NewBlockIngestionRepo().ExpectedTransfer(workCtx, payment)
				} else {
					processErr = crypto.ErrVerificationPending
				}
			}
			if processErr == nil {
				proof, err := verifier.VerifyTransfer(workCtx, event)
				if err != nil {
					processErr = err
				} else {
					proofID, err := crypto_repo.NewBlockIngestionRepo().SaveVerification(workCtx, proof)
					if err != nil {
						processErr = err
					} else {
						event = proof.Event
						event.VerificationID = &proofID
					}
				}
			}
		}
	}
	if processErr == nil {
		processErr = (contextTxManager{}).RunInTransaction(workCtx, func(txCtx context.Context) error {
			if err := p.eventProcessor.ProcessEvent(txCtx, event); err != nil {
				return err
			}
			return repo.Complete(txCtx, observation.ID, p.workerID)
		})
	}
	if processErr == nil {
		return nil
	}
	if errors.Is(processErr, crypto.ErrExternalOutflowBusy) {
		return (contextTxManager{}).RunInTransaction(workCtx, func(txCtx context.Context) error { return repo.DeferExternalOutflow(txCtx, observation.ID, p.workerID) })
	}
	if errors.Is(processErr, crypto.ErrVerificationPending) || errors.Is(processErr, crypto.ErrChainQuorumUnavailable) {
		return (contextTxManager{}).RunInTransaction(workCtx, func(txCtx context.Context) error { return repo.DeferVerification(txCtx, observation.ID, p.workerID) })
	}
	if errors.Is(processErr, crypto.ErrVerificationMismatch) {
		if err := crypto_repo.NewBlockIngestionRepo().ReportIngestionFailure(workCtx, observation.NetworkID, "observation:"+observation.ID.String(), event.BlockNumber, "transfer_mismatch", true); err != nil {
			return err
		}
	}
	span.RecordError(processErr)
	span.SetStatus(codes.Error, "observation processing failed")
	p.log.Errorw("failed to process chain observation",
		"observation_id", observation.ID, "network_id", observation.NetworkID,
		"attempts", observation.Attempts, "error", processErr)
	if err := (contextTxManager{}).RunInTransaction(workCtx, func(txCtx context.Context) error {
		needsReview, err := repo.Fail(txCtx, observation.ID, p.workerID, processErr)
		if err != nil {
			return err
		}
		if !needsReview {
			return nil
		}
		holds := cryptoapp.NewOperationalHoldService(contextTxManager{}, crypto_repo.NewOperationalHoldRepo())
		return holds.Acquire(txCtx, cryptoapp.AcquireOperationalHoldCommand{
			Scope: cryptoapp.OperationalHoldScopeNetwork, ScopeID: observation.NetworkID,
			Reason: "chain_observation_needs_review", OwnerType: "chain_observation",
			OwnerID: observation.ID.String(), HeldBy: "crypto-worker",
			Details: map[string]any{"observationId": observation.ID.String(), "attempts": observation.Attempts},
		})
	}); err != nil {
		return fmt.Errorf("record failed chain observation: %w", err)
	}
	return nil
}
