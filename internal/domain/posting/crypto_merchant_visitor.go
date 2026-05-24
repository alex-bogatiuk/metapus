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

// ValidateBeforePost implements PostingValidator — checks merchant balance availability
// for expense movements with pessimistic locking (FOR UPDATE + resource ordering).
// Analogous to StockRecorder.ValidateBeforePost.
func (r *CryptoMerchantBalanceRecorder) ValidateBeforePost(ctx context.Context, set *MovementSet) error {
	raw, ok := set.GetExtension(_cryptoMerchantBalanceExtKey)
	if !ok {
		return nil
	}

	movements, ok := raw.([]entity.CryptoMerchantBalanceMovement)
	if !ok {
		return nil
	}

	return validateMerchantBalance(r.service, ctx, movements)
}

// validateMerchantBalance checks if there is enough merchant balance for expense movements.
// Groups movements by merchant+token, then delegates to Service.CheckAndReserveMerchantBalance.
func validateMerchantBalance(
	svc *crypto_merchant_balance.Service,
	ctx context.Context,
	movements []entity.CryptoMerchantBalanceMovement,
) error {
	type dimKey struct {
		merchantID, tokenID id.ID
	}
	reserves := make(map[dimKey]*crypto_merchant_balance.MerchantBalanceReservation)

	for _, m := range movements {
		if m.RecordType != entity.RecordTypeExpense {
			continue
		}

		key := dimKey{m.MerchantID, m.TokenID}
		if existing, ok := reserves[key]; ok {
			existing.Required = existing.Required.Add(m.Amount)
		} else {
			reserves[key] = &crypto_merchant_balance.MerchantBalanceReservation{
				MerchantID: m.MerchantID,
				TokenID:    m.TokenID,
				Required:   m.Amount,
			}
		}
	}

	if len(reserves) == 0 {
		return nil
	}

	items := make([]crypto_merchant_balance.MerchantBalanceReservation, 0, len(reserves))
	for _, r := range reserves {
		items = append(items, *r)
	}

	return svc.CheckAndReserveMerchantBalance(ctx, items)
}

// Compile-time interface checks.
var _ PostingValidator = (*CryptoMerchantBalanceRecorder)(nil)
