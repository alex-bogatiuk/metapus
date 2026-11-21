package crypto_worker

import (
	"context"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/types"
)

type feeBudgetObserver struct{}

func (feeBudgetObserver) Reserve(ctx context.Context, req FeeReservationRequest) (context.Context, func(error, *types.RawAmount)) {
	ctx, span := startCryptoPhase(ctx, "crypto.fee.reserve", executionAttributes(req.NetworkID, req.Purpose)...)
	return ctx, func(err error, remaining *types.RawAmount) {
		recordFeeReservation(ctx, req, err, remaining)
		finishCryptoPhase(span, err)
	}
}
func (feeBudgetObserver) Settle(ctx context.Context, receipt crypto.ChainReceipt) (context.Context, func(crypto.SigningPurpose, time.Time, error)) {
	ctx, span := startCryptoPhase(ctx, "crypto.fee.settle", executionAttributes(receipt.NetworkID, "")...)
	return ctx, func(purpose crypto.SigningPurpose, reservedAt time.Time, err error) {
		recordFeeSettlement(ctx, receipt, purpose, reservedAt, err)
		finishCryptoPhase(span, err)
	}
}
