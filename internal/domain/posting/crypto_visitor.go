package posting

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/registers/crypto_balance"
)

// ---------------------------------------------------------------------------
// Crypto Balance register — Visitor + Recorder
// ---------------------------------------------------------------------------

// CryptoBalanceMovementSource is implemented by documents that generate
// crypto balance register movements (e.g., CryptoPayment, CryptoWithdrawal).
type CryptoBalanceMovementSource interface {
	GenerateCryptoBalanceMovements(ctx context.Context) ([]entity.CryptoBalanceMovement, error)
}

const _cryptoBalanceExtKey = "crypto_balance"

// CryptoBalanceVisitor collects crypto balance movements from documents
// that implement CryptoBalanceMovementSource.
type CryptoBalanceVisitor struct{}

// Name implements RegisterVisitor.
func (v *CryptoBalanceVisitor) Name() string { return _cryptoBalanceExtKey }

// CollectMovements implements RegisterVisitor.
func (v *CryptoBalanceVisitor) CollectMovements(ctx context.Context, doc Postable, set *MovementSet) error {
	src, ok := doc.(CryptoBalanceMovementSource)
	if !ok {
		return nil
	}

	movements, err := src.GenerateCryptoBalanceMovements(ctx)
	if err != nil {
		return fmt.Errorf("generate crypto balance movements: %w", err)
	}

	if len(movements) > 0 {
		set.SetExtension(_cryptoBalanceExtKey, movements)
	}
	return nil
}

// CryptoBalanceRecorder adapts crypto_balance.Service into a RegisterRecorder.
type CryptoBalanceRecorder struct {
	service *crypto_balance.Service
}

// NewCryptoBalanceRecorder creates a new CryptoBalanceRecorder.
func NewCryptoBalanceRecorder(s *crypto_balance.Service) *CryptoBalanceRecorder {
	return &CryptoBalanceRecorder{service: s}
}

func (r *CryptoBalanceRecorder) Name() string { return _cryptoBalanceExtKey }

func (r *CryptoBalanceRecorder) RecordFromSet(ctx context.Context, set *MovementSet) error {
	raw, ok := set.GetExtension(_cryptoBalanceExtKey)
	if !ok {
		return nil
	}

	movements, ok := raw.([]entity.CryptoBalanceMovement)
	if !ok || len(movements) == 0 {
		return nil
	}

	return r.service.RecordMovements(ctx, movements)
}

func (r *CryptoBalanceRecorder) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.service.ReverseMovements(ctx, recorderID, beforeVersion)
}
