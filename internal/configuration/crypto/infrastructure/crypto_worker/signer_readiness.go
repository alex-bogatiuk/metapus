package crypto_worker

import (
	"context"
	"fmt"

	"metapus/internal/configuration/crypto/domain/crypto"
)

type signingAuthorizationReadiness interface {
	CheckReady(ctx context.Context) (string, error)
}

func resolveOutboundSigner(
	ctx context.Context,
	signers *crypto.FamilySignerRegistry,
	authorizations signingAuthorizationReadiness,
	network crypto.NetworkRef,
) (resolved crypto.PolicyAwareTransactionSigner, err error) {
	ctx, span := startCryptoPhase(ctx, "crypto.signing.family_readiness", networkAttributes(network)...)
	defer func() {
		recordSignerCheck(ctx, network.Family, err == nil)
		finishCryptoPhase(span, err)
	}()
	if signers == nil {
		return nil, fmt.Errorf("family signing registry is not configured")
	}
	if authorizations == nil {
		return nil, fmt.Errorf("signing authorization service is not configured")
	}
	signer, err := signers.Resolve(network.Family)
	if err != nil {
		return nil, err
	}
	kid, err := authorizations.CheckReady(ctx)
	if err != nil {
		return nil, err
	}
	if err := signer.CheckReady(ctx, network, kid); err != nil {
		return nil, fmt.Errorf("%s signer is not ready: %w", network.Family, err)
	}
	if custody, ok := authorizations.(interface {
		CheckCustodySigner(context.Context, crypto.NetworkRef, crypto.PolicyAwareTransactionSigner) error
	}); ok {
		if err := custody.CheckCustodySigner(ctx, network, signer); err != nil {
			return nil, err
		}
	}
	return signer, nil
}

func (s *SigningAuthorizationService) ResolveSigner(ctx context.Context, registry *crypto.FamilySignerRegistry, network crypto.NetworkRef) (crypto.PolicyAwareTransactionSigner, error) {
	return resolveOutboundSigner(ctx, registry, s, network)
}
