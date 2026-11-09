package crypto_worker

import (
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres/outbound_repo"
)

var (
	ErrNetworkPolicyDisabled  = crypto.ErrNetworkPolicyDisabled
	ErrNetworkOperationalHold = crypto.ErrNetworkOperationalHold
	ErrFeeBudgetExceeded      = crypto.ErrFeeBudgetExceeded
	ErrFeeBalanceResidual     = crypto.ErrFeeBalanceResidual
	ErrFeeReservationHeld     = crypto.ErrFeeReservationHeld
)

type FeeReservationRequest = outbound_repo.FeeReservationRequest

type FeeBudgetManager = outbound_repo.FeeBudgetManager

func NewFeeBudgetManager() *FeeBudgetManager {
	return outbound_repo.NewFeeBudgetManager(feeBudgetObserver{})
}

type FeeSettlementResult = outbound_repo.FeeSettlementResult
