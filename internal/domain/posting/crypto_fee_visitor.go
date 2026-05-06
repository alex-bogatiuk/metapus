package posting

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/registers/crypto_fee"
)

// ---------------------------------------------------------------------------
// Crypto Fee register — Visitor + Recorder
// ---------------------------------------------------------------------------

// CryptoFeeMovementSource is implemented by documents that generate
// crypto fee register movements (e.g., CryptoPayment, CryptoWithdrawal).
type CryptoFeeMovementSource interface {
	GenerateCryptoFeeMovements(ctx context.Context) ([]entity.CryptoFeeMovement, error)
}

const _cryptoFeeExtKey = "crypto_fee"

// CryptoFeeVisitor collects crypto fee movements from documents
// that implement CryptoFeeMovementSource.
type CryptoFeeVisitor struct{}

// Name implements RegisterVisitor.
func (v *CryptoFeeVisitor) Name() string { return _cryptoFeeExtKey }

// CollectMovements implements RegisterVisitor.
func (v *CryptoFeeVisitor) CollectMovements(ctx context.Context, doc Postable, set *MovementSet) error {
	src, ok := doc.(CryptoFeeMovementSource)
	if !ok {
		return nil
	}

	movements, err := src.GenerateCryptoFeeMovements(ctx)
	if err != nil {
		return fmt.Errorf("generate crypto fee movements: %w", err)
	}

	if len(movements) > 0 {
		set.SetExtension(_cryptoFeeExtKey, movements)
	}
	return nil
}

// CryptoFeeRecorder adapts crypto_fee.Service into a RegisterRecorder.
type CryptoFeeRecorder struct {
	service *crypto_fee.Service
}

// NewCryptoFeeRecorder creates a new CryptoFeeRecorder.
func NewCryptoFeeRecorder(s *crypto_fee.Service) *CryptoFeeRecorder {
	return &CryptoFeeRecorder{service: s}
}

func (r *CryptoFeeRecorder) Name() string { return _cryptoFeeExtKey }

func (r *CryptoFeeRecorder) RecordFromSet(ctx context.Context, set *MovementSet) error {
	raw, ok := set.GetExtension(_cryptoFeeExtKey)
	if !ok {
		return nil
	}

	movements, ok := raw.([]entity.CryptoFeeMovement)
	if !ok || len(movements) == 0 {
		return nil
	}

	return r.service.RecordMovements(ctx, movements)
}

func (r *CryptoFeeRecorder) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.service.ReverseMovements(ctx, recorderID, beforeVersion)
}
