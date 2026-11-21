package cryptoposting

import (
	"context"
	"fmt"

	"metapus/internal/configuration/crypto/domain/registers/crypto_fee"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/posting"
)

// ---------------------------------------------------------------------------
// Crypto Fee register — Visitor + Recorder
// ---------------------------------------------------------------------------

// CryptoFeeMovementSource is implemented by documents that generate
// crypto fee register movements (e.g., CryptoPayment, CryptoWithdrawal).
type CryptoFeeMovementSource interface {
	GenerateCryptoFeeMovements(ctx context.Context) ([]crypto_fee.CryptoFeeMovement, error)
}

const _cryptoFeeExtKey = "crypto_fee"

// CryptoFeeVisitor collects crypto fee movements from documents
// that implement CryptoFeeMovementSource.
type CryptoFeeVisitor struct{}

// Name implements posting.RegisterVisitor.
func (v *CryptoFeeVisitor) Name() string { return _cryptoFeeExtKey }

// CollectMovements implements posting.RegisterVisitor.
func (v *CryptoFeeVisitor) CollectMovements(ctx context.Context, doc posting.Postable, set *posting.MovementSet) error {
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

// CryptoFeeRecorder adapts crypto_fee.Service into a posting.RegisterRecorder.
type CryptoFeeRecorder struct {
	service *crypto_fee.Service
}

// NewCryptoFeeRecorder creates a new CryptoFeeRecorder.
func NewCryptoFeeRecorder(s *crypto_fee.Service) *CryptoFeeRecorder {
	return &CryptoFeeRecorder{service: s}
}

func (r *CryptoFeeRecorder) Name() string { return _cryptoFeeExtKey }

func (r *CryptoFeeRecorder) RecordFromSet(ctx context.Context, set *posting.MovementSet) error {
	raw, ok := set.GetExtension(_cryptoFeeExtKey)
	if !ok {
		return nil
	}

	movements, ok := raw.([]crypto_fee.CryptoFeeMovement)
	if !ok || len(movements) == 0 {
		return nil
	}

	return r.service.RecordMovements(ctx, movements)
}

func (r *CryptoFeeRecorder) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.service.ReverseMovements(ctx, recorderID, beforeVersion)
}

// ValidateBeforePost checks platform fee balance availability for expense movements.
func (r *CryptoFeeRecorder) ValidateBeforePost(ctx context.Context, set *posting.MovementSet) error {
	raw, ok := set.GetExtension(_cryptoFeeExtKey)
	if !ok {
		return nil
	}

	movements, ok := raw.([]crypto_fee.CryptoFeeMovement)
	if !ok {
		return nil
	}

	type dimKey struct {
		m, t id.ID
		ft   crypto_fee.FeeType
	}
	reserves := make(map[dimKey]*crypto_fee.BalanceReservation)
	for _, m := range movements {
		if m.RecordType != entity.RecordTypeExpense {
			continue
		}
		key := dimKey{m.MerchantID, m.TokenID, m.FeeType}
		if existing, ok := reserves[key]; ok {
			existing.Required = existing.Required.Add(m.Amount)
		} else {
			reserves[key] = &crypto_fee.BalanceReservation{
				MerchantID: m.MerchantID,
				TokenID:    m.TokenID,
				FeeType:    m.FeeType,
				Required:   m.Amount,
			}
		}
	}
	if len(reserves) == 0 {
		return nil
	}

	items := make([]crypto_fee.BalanceReservation, 0, len(reserves))
	for _, item := range reserves {
		items = append(items, *item)
	}
	return r.service.CheckAndReserveCryptoFee(ctx, items)
}
