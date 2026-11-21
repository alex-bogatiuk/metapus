package outboundapp

import (
	"context"
	"errors"
	"fmt"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"time"
)

func resourceCircuitValues(policy crypto.NetworkPolicy) (int, time.Duration) {
	if policy.ExternalResources == nil {
		return 0, 0
	}
	return policy.ExternalResources.ProviderFailureThreshold, policy.ExternalResources.ProviderOpenDuration
}

// prepareResourceByPolicy keeps provider-specific decisions out of generic
// executors. An ambiguous order is always fail-closed; fallback is allowed only
// when no provider charge can have occurred.
func PrepareResourceByPolicy(
	ctx context.Context,
	preparer ResourcePreparer,
	policy crypto.NetworkPolicy,
	needsResource bool,
	req ResourceRequest,
) (result ResourceReservationResult, strategy crypto.FeeStrategy, err error) {
	resourcePolicy := policy.ExternalResources
	if resourcePolicy != nil {
		if len(req.ProviderAccountIDs) == 0 {
			req.ProviderAccountIDs = append([]id.ID(nil), resourcePolicy.ProviderAccountIDs...)
		}
	}
	if !needsResource {
		return ResourceReservationResult{}, crypto.FeeStrategyNativeBurn, nil
	}
	if resourcePolicy == nil {
		return ResourceReservationResult{}, "", errors.New("chain resource requirements have no family resource policy")
	}
	if resourcePolicy.Strategy == crypto.ResourceBurnOnly {
		return ResourceReservationResult{}, crypto.FeeStrategyNativeBurn, nil
	}
	if preparer == nil {
		return ResourceReservationResult{}, "", ErrResourceRentalRequired
	}
	resource, err := preparer.PrepareResource(ctx, req)
	if err == nil && resource.ResourceUsed {
		return resource, crypto.FeeStrategyResourceRental, nil
	}
	if err == nil {
		err = ErrResourceCapacityInsufficient
	}
	if resourcePolicy.Strategy == crypto.ResourceRentalRequired {
		return ResourceReservationResult{}, "", fmt.Errorf("%w: %v", ErrResourceRentalRequired, err)
	}
	if errors.Is(err, crypto.ErrResourceOrderAmbiguous) || errors.Is(err, crypto.ErrResourceProviderUnauthorized) || errors.Is(err, crypto.ErrResourceProviderInvalidReply) {
		return ResourceReservationResult{}, "", err
	}
	if errors.Is(err, ErrResourceCapacityInsufficient) || errors.Is(err, crypto.ErrResourceProviderTransient) {
		return ResourceReservationResult{}, crypto.FeeStrategyNativeBurn, nil
	}
	return ResourceReservationResult{}, "", err
}

func prepareObservedResource(ctx context.Context, observer Observer, preparer ResourcePreparer, policy crypto.NetworkPolicy, needsResource bool, req ResourceRequest) (result ResourceReservationResult, strategy crypto.FeeStrategy, err error) {
	if observer != nil {
		var finish func(crypto.FeeStrategy, error)
		ctx, finish = observer.ResourcePreparation(ctx, policy)
		defer func() { finish(strategy, err) }()
	}
	return PrepareResourceByPolicy(ctx, preparer, policy, needsResource, req)
}
