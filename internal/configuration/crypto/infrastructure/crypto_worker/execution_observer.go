package crypto_worker

import (
	"context"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type executionObserver struct{}

func (executionObserver) Start(ctx context.Context, name string, network id.ID, purpose crypto.SigningPurpose) (context.Context, func(error)) {
	ctx, span := startExecutionSpan(ctx, name, network, purpose)
	return ctx, func(err error) { finishExecutionSpan(ctx, span, network, purpose, err) }
}
func (executionObserver) Broadcast(ctx context.Context, network crypto.NetworkRef, purpose crypto.SigningPurpose, outcome crypto.BroadcastOutcome) {
	recordBroadcastOutcome(ctx, network, purpose, outcome)
}

func (executionObserver) ResourcePreparation(ctx context.Context, policy crypto.NetworkPolicy) (context.Context, func(crypto.FeeStrategy, error)) {
	selected := crypto.ResourceBurnOnly
	if policy.ExternalResources != nil {
		selected = policy.ExternalResources.Strategy
	}
	ctx, span := startCryptoPhase(ctx, "crypto.resource.prepare")
	return ctx, func(strategy crypto.FeeStrategy, err error) {
		recordResourceDecision(ctx, policy.NetworkID, selected, strategy, err)
		finishCryptoPhase(span, err)
	}
}

func (executionObserver) FundingFinality(ctx context.Context, network crypto.NetworkRef) (context.Context, func(error)) {
	ctx, span := startCryptoPhase(ctx, "crypto.native_funding.finality", networkAttributes(network)...)
	return ctx, func(err error) { finishCryptoPhase(span, err) }
}
func (executionObserver) FundingSettled(ctx context.Context, network id.ID, amount types.RawAmount) {
	recordNativeFunding(ctx, network, amount)
}

func (executionObserver) Finality(ctx context.Context, network crypto.NetworkRef) (context.Context, func(error)) {
	ctx, span := startCryptoPhase(ctx, "crypto.finality.read", networkAttributes(network)...)
	return ctx, func(err error) { finishCryptoPhase(span, err) }
}
func (executionObserver) Replacement(ctx context.Context, network id.ID, generation int, event string) {
	recordReplacementEvent(ctx, network, generation, event)
}
