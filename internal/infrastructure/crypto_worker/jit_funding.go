package crypto_worker

import (
	"context"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/application/outboundapp"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/outbound_repo"
)

var ErrNativeFundingPending = outbound_repo.ErrNativeFundingPending
var ErrNativeFundingHeld = outbound_repo.ErrNativeFundingHeld

// NativeFundingRequest identifies the managed wallet whose native balance must
// cover a token transfer's bounded network fee. BasisType/BasisID provide the
// durable idempotency identity of the parent outbound operation.
type NativeFundingRequest = outboundapp.NativeFundingRequest

type NativeFundingService struct {
	*outboundapp.NativeFundingService
}

func NewNativeFundingService(repo *OutboundRepo, fees *FeeBudgetManager, auth *SigningAuthorizationService) *NativeFundingService {
	var store outboundapp.NativeFundingRepository
	if repo != nil {
		store = nativeFundingStore{OutboundRepo: repo}
	}
	var guard outboundapp.FundingAuthorizations
	if auth != nil {
		guard = auth
	}
	var settlement outboundapp.FundingSettlement
	if fees != nil {
		settlement = cryptoapp.NewNativeFundingSettlementService(contextTxManager{}, nativeFundingSettlementRepo{}, fees)
	}
	evidence := outboundapp.NewNativeFundingEvidenceService(crypto_repo.NativeFundingEvidenceRepo{BlockIngestionRepo: crypto_repo.NewBlockIngestionRepo()})
	return &NativeFundingService{outboundapp.NewNativeFundingService(store, custodyService(), feeBudgetPort(fees), guard, settlement, executionObserver{}, evidence)}
}

type nativeFundingSettlementRepo = outbound_repo.NativeFundingSettlementRepo

func (s *NativeFundingService) Ensure(ctx context.Context, driver crypto.NetworkDriver, policy crypto.NetworkPolicy, request NativeFundingRequest, signer crypto.PolicyAwareTransactionSigner, requiredFee types.CryptoAmount) (bool, error) {
	var service *outboundapp.NativeFundingService
	if s != nil {
		service = s.NativeFundingService
	}
	return service.Ensure(ctx, driver, policy, request, signer, requiredFee)
}
