package crypto

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/pkg/logger"
)

// SettlementStrategy defines how merchant funds are settled.
// v1: CryptoSettlementStrategy (crypto-to-crypto, direct transfer)
// Future: FiatSettlementStrategy (OTC/exchange conversion to fiat)
type SettlementStrategy interface {
	// Settle transfers the given amount to the merchant.
	// Called after sweep is confirmed and fees are deducted.
	Settle(ctx context.Context, merchantID id.ID, amount types.CryptoAmount, tokenID id.ID) error
}

// CryptoSettlementStrategy implements crypto-to-crypto settlement.
// Funds are transferred directly in the same token — no conversion needed.
// This is the simplest settlement: subtract fees, record net amount.
type CryptoSettlementStrategy struct {
	webhookDispatcher *WebhookDispatcher
}

// NewCryptoSettlementStrategy creates a new crypto-to-crypto settlement strategy.
func NewCryptoSettlementStrategy(webhookDispatcher *WebhookDispatcher) *CryptoSettlementStrategy {
	return &CryptoSettlementStrategy{
		webhookDispatcher: webhookDispatcher,
	}
}

// Settle implements SettlementStrategy for crypto-to-crypto.
// In v1, settlement is immediate: the withdrawal document IS the settlement.
// This method records the settlement event and triggers the merchant webhook.
func (s *CryptoSettlementStrategy) Settle(ctx context.Context, merchantID id.ID, amount types.CryptoAmount, tokenID id.ID) error {
	logger.Info(ctx, "crypto settlement executed",
		"merchant_id", merchantID,
		"amount", amount.String(),
		"token_id", tokenID,
	)

	// Settlement in v1 is a no-op: the CryptoWithdrawal already represents
	// the fund transfer. This method exists for:
	// 1. Future fee deduction logic
	// 2. Settlement record keeping in reg_settlement
	// 3. Webhook notification to merchant

	return nil
}

// Compile-time interface check.
var _ SettlementStrategy = (*CryptoSettlementStrategy)(nil)

// FiatSettlementStrategy is a placeholder for future crypto-to-fiat settlement.
// Will integrate with OTC desk or exchange adapter for conversion.
// type FiatSettlementStrategy struct {
//     exchange   ExchangeAdapter
//     bankClient BankTransferClient
// }

// SettlementService orchestrates the settlement process.
type SettlementService struct {
	strategy SettlementStrategy
}

// NewSettlementService creates a new settlement service.
func NewSettlementService(strategy SettlementStrategy) *SettlementService {
	return &SettlementService{strategy: strategy}
}

// SettleMerchant settles funds for a merchant.
func (s *SettlementService) SettleMerchant(ctx context.Context, merchantID id.ID, amount types.CryptoAmount, tokenID id.ID) error {
	if amount.IsZero() {
		return nil
	}

	if err := s.strategy.Settle(ctx, merchantID, amount, tokenID); err != nil {
		return fmt.Errorf("settle merchant %s: %w", merchantID, err)
	}

	return nil
}
