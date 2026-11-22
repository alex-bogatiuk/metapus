package outbound_repo

import (
	"context"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/types"
)

type FeeBudgetObserver interface {
	Reserve(context.Context, FeeReservationRequest) (context.Context, func(error, *types.RawAmount))
	Settle(context.Context, crypto.ChainReceipt) (context.Context, func(crypto.SigningPurpose, time.Time, error))
}

func NewOutboundRepo() *OutboundRepo { return &OutboundRepo{} }
func NewFeeBudgetManager(observer FeeBudgetObserver) *FeeBudgetManager {
	return &FeeBudgetManager{observer: observer}
}
