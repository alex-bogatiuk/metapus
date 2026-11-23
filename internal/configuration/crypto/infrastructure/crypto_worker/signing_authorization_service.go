package crypto_worker

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
)

type SigningAuthorizationService struct {
	service *outboundapp.SigningAuthorizationService
}
type SigningAttemptInput = outboundapp.SigningAttemptInput

func NewSigningAuthorizationService(repo *OutboundRepo, signer crypto.SigningAuthorizationSigner, tenantID string) (*SigningAuthorizationService, error) {
	var store outboundapp.SigningAuthorizationRepository
	if repo != nil {
		store = repo
	}
	service, err := outboundapp.NewSigningAuthorizationService(store, signer, custodyService(), tenantID, time.Now)
	if err != nil {
		return nil, err
	}
	return &SigningAuthorizationService{service}, nil
}
func (s *SigningAuthorizationService) CheckReady(ctx context.Context) (string, error) {
	ctx, span := startCryptoPhase(ctx, "crypto.signing.authorization_readiness")
	kid, err := s.service.CheckReady(ctx)
	recordSignerCheck(ctx, "authorization", err == nil)
	finishCryptoPhase(span, err)
	return kid, err
}

func (s *SigningAuthorizationService) Create(ctx context.Context, input SigningAttemptInput) (SigningAuthorizationRecord, error) {
	ctx, span := startCryptoPhase(ctx, "crypto.signing.authorize", networkAttributes(input.Intent.Network)...)
	record, err := s.service.Create(ctx, input)
	recordSigningOperation(ctx, input.Intent.Network.Family, "authorize", false, err)
	finishCryptoPhase(span, err)
	return record, err
}

func (s *SigningAuthorizationService) SignOrReuse(ctx context.Context, signer crypto.PolicyAwareTransactionSigner, record SigningAuthorizationRecord) (crypto.SignedTxEnvelope, error) {
	attrs := []attribute.KeyValue{
		attribute.String("crypto.chain.family", string(record.Authorization.ChainFamily)),
		attribute.String("crypto.purpose", string(record.Authorization.Purpose)),
	}
	ctx, span := startCryptoPhase(ctx, "crypto.signing.transaction", attrs...)
	reused := record.SignedTx != nil
	signed, err := s.service.SignOrReuse(ctx, signer, record)
	recordSigningOperation(ctx, record.Authorization.ChainFamily, "transaction", reused, err)
	finishCryptoPhase(span, err)
	return signed, err
}

func (s *SigningAuthorizationService) CreateGuarded(ctx context.Context, input SigningAttemptInput, guard outboundapp.SigningExecutionGuard) (SigningAuthorizationRecord, error) {
	ctx, span := startCryptoPhase(ctx, "crypto.signing.authorize", networkAttributes(input.Intent.Network)...)
	record, err := s.service.CreateGuarded(ctx, input, guard)
	recordSigningOperation(ctx, input.Intent.Network.Family, "authorize", false, err)
	finishCryptoPhase(span, err)
	return record, err
}
func (s *SigningAuthorizationService) SignOrReuseGuarded(ctx context.Context, signer crypto.PolicyAwareTransactionSigner, record SigningAuthorizationRecord, guard outboundapp.SigningExecutionGuard) (crypto.SignedTxEnvelope, error) {
	attrs := []attribute.KeyValue{attribute.String("crypto.chain.family", string(record.Authorization.ChainFamily)), attribute.String("crypto.purpose", string(record.Authorization.Purpose))}
	ctx, span := startCryptoPhase(ctx, "crypto.signing.transaction", attrs...)
	signed, err := s.service.SignOrReuseGuarded(ctx, signer, record, guard)
	recordSigningOperation(ctx, record.Authorization.ChainFamily, "transaction", record.SignedTx != nil, err)
	finishCryptoPhase(span, err)
	return signed, err
}
func (s *SigningAuthorizationService) CheckCustodyDispatchGuarded(ctx context.Context, auth crypto.SigningAuthorization, registry *crypto.FamilySignerRegistry, guard outboundapp.SigningExecutionGuard) error {
	return custodyService().CheckCustodyDispatchGuarded(ctx, auth, registry, guard)
}
