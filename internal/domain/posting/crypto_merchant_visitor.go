package posting

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/registers/crypto_merchant_balance"
)

// ---------------------------------------------------------------------------
// Crypto Merchant Balance register — Visitor + Recorder
// ---------------------------------------------------------------------------

// CryptoMerchantBalanceMovementSource is implemented by documents that generate
// crypto merchant balance register movements (e.g., CryptoPayment, CryptoWithdrawal).
// Receipt = platform owes more, Expense = debt reduced (withdrawal).
type CryptoMerchantBalanceMovementSource interface {
	GenerateCryptoMerchantBalanceMovements(ctx context.Context) ([]entity.CryptoMerchantBalanceMovement, error)
}

const _cryptoMerchantBalanceExtKey = "crypto_merchant_balance"

// CryptoMerchantBalanceVisitor collects crypto merchant balance movements from documents
// that implement CryptoMerchantBalanceMovementSource.
type CryptoMerchantBalanceVisitor struct{}

// Name implements RegisterVisitor.
func (v *CryptoMerchantBalanceVisitor) Name() string { return _cryptoMerchantBalanceExtKey }

// CollectMovements implements RegisterVisitor.
func (v *CryptoMerchantBalanceVisitor) CollectMovements(ctx context.Context, doc Postable, set *MovementSet) error {
	src, ok := doc.(CryptoMerchantBalanceMovementSource)
	if !ok {
		return nil
	}

	movements, err := src.GenerateCryptoMerchantBalanceMovements(ctx)
	if err != nil {
		return fmt.Errorf("generate crypto merchant balance movements: %w", err)
	}

	if len(movements) > 0 {
		set.SetExtension(_cryptoMerchantBalanceExtKey, movements)
	}
	return nil
}

// CryptoMerchantBalanceRecorder adapts crypto_merchant_balance.Service into a RegisterRecorder.
type CryptoMerchantBalanceRecorder struct {
	service *crypto_merchant_balance.Service
}

// NewCryptoMerchantBalanceRecorder creates a new CryptoMerchantBalanceRecorder.
func NewCryptoMerchantBalanceRecorder(s *crypto_merchant_balance.Service) *CryptoMerchantBalanceRecorder {
	return &CryptoMerchantBalanceRecorder{service: s}
}

func (r *CryptoMerchantBalanceRecorder) Name() string { return _cryptoMerchantBalanceExtKey }

func (r *CryptoMerchantBalanceRecorder) RecordFromSet(ctx context.Context, set *MovementSet) error {
	raw, ok := set.GetExtension(_cryptoMerchantBalanceExtKey)
	if !ok {
		return nil
	}

	movements, ok := raw.([]entity.CryptoMerchantBalanceMovement)
	if !ok || len(movements) == 0 {
		return nil
	}

	return r.service.RecordMovements(ctx, movements)
}

func (r *CryptoMerchantBalanceRecorder) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.service.ReverseMovements(ctx, recorderID, beforeVersion)
}
