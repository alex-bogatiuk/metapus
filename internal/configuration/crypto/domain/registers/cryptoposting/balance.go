package cryptoposting

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/registers/crypto_balance"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/posting"
)

// ---------------------------------------------------------------------------
// Crypto Balance register — Visitor + Recorder
// ---------------------------------------------------------------------------

// CryptoBalanceMovementSource is implemented by documents that generate
// crypto balance register movements (e.g., CryptoPayment, CryptoWithdrawal).
type CryptoBalanceMovementSource interface {
	GenerateCryptoBalanceMovements(ctx context.Context) ([]crypto_balance.CryptoBalanceMovement, error)
}

const _cryptoBalanceExtKey = "crypto_balance"

func CryptoBalanceMovements(set *posting.MovementSet) ([]crypto_balance.CryptoBalanceMovement, error) {
	raw, ok := set.GetExtension(_cryptoBalanceExtKey)
	if !ok {
		return nil, nil
	}
	rows, ok := raw.([]crypto_balance.CryptoBalanceMovement)
	if !ok {
		return nil, fmt.Errorf("invalid crypto balance movement set")
	}
	return rows, nil
}

// CryptoBalanceVisitor collects crypto balance movements from documents
// that implement CryptoBalanceMovementSource.
type CryptoBalanceVisitor struct{}

// Name implements posting.RegisterVisitor.
func (v *CryptoBalanceVisitor) Name() string { return _cryptoBalanceExtKey }

// CollectMovements implements posting.RegisterVisitor.
func (v *CryptoBalanceVisitor) CollectMovements(ctx context.Context, doc posting.Postable, set *posting.MovementSet) error {
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

// CryptoBalanceRecorder adapts crypto_balance.Service into a posting.RegisterRecorder.
type CryptoBalanceRecorder struct {
	service *crypto_balance.Service
}

// NewCryptoBalanceRecorder creates a new CryptoBalanceRecorder.
func NewCryptoBalanceRecorder(s *crypto_balance.Service) *CryptoBalanceRecorder {
	return &CryptoBalanceRecorder{service: s}
}

func (r *CryptoBalanceRecorder) Name() string { return _cryptoBalanceExtKey }

func (r *CryptoBalanceRecorder) LockPostingResources(ctx context.Context, doc posting.Postable) error {
	var movements []crypto_balance.CryptoBalanceMovement
	if source, ok := doc.(CryptoBalanceMovementSource); ok {
		var err error
		movements, err = source.GenerateCryptoBalanceMovements(ctx)
		if err != nil {
			return err
		}
	}
	return r.service.LockPostingResources(ctx, doc.GetID(), movements)
}

func (r *CryptoBalanceRecorder) LockReversalResources(ctx context.Context, recorderID id.ID) error {
	return r.service.LockPostingResources(ctx, recorderID, nil)
}

var _ posting.PostingResourceLocker = (*CryptoBalanceRecorder)(nil)

func (r *CryptoBalanceRecorder) RecordFromSet(ctx context.Context, set *posting.MovementSet) error {
	raw, ok := set.GetExtension(_cryptoBalanceExtKey)
	if !ok {
		return nil
	}

	movements, ok := raw.([]crypto_balance.CryptoBalanceMovement)
	if !ok || len(movements) == 0 {
		return nil
	}

	return r.service.RecordMovements(ctx, movements)
}

func (r *CryptoBalanceRecorder) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.service.ReverseMovements(ctx, recorderID, beforeVersion)
}

// ValidateBeforePost implements posting.PostingValidator — checks crypto wallet balance
// availability for expense movements with pessimistic locking.
func (r *CryptoBalanceRecorder) ValidateBeforePost(ctx context.Context, set *posting.MovementSet) error {
	raw, ok := set.GetExtension(_cryptoBalanceExtKey)
	if !ok {
		return nil
	}

	movements, ok := raw.([]crypto_balance.CryptoBalanceMovement)
	if !ok {
		return nil
	}

	return validateCryptoBalance(r.service, ctx, movements)
}

func validateCryptoBalance(
	svc *crypto_balance.Service,
	ctx context.Context,
	movements []crypto_balance.CryptoBalanceMovement,
) error {
	type dimKey struct {
		walletID, tokenID id.ID
	}
	reserves := make(map[dimKey]*crypto_balance.BalanceReservation)

	for _, m := range movements {
		if m.RecordType != entity.RecordTypeExpense {
			continue
		}

		key := dimKey{m.WalletID, m.TokenID}
		if existing, ok := reserves[key]; ok {
			existing.Required = existing.Required.Add(m.Amount)
		} else {
			reserves[key] = &crypto_balance.BalanceReservation{
				WalletID: m.WalletID,
				TokenID:  m.TokenID,
				Required: m.Amount,
			}
		}
	}

	if len(reserves) == 0 {
		return nil
	}

	items := make([]crypto_balance.BalanceReservation, 0, len(reserves))
	for _, r := range reserves {
		items = append(items, *r)
	}

	return svc.CheckAndReserveCryptoBalance(ctx, items)
}

var _ posting.PostingValidator = (*CryptoBalanceRecorder)(nil)
