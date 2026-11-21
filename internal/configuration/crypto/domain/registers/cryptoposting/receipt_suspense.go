package cryptoposting

import (
	"context"
	"fmt"

	"metapus/internal/configuration/crypto/domain/registers/crypto_receipt_suspense"
	"metapus/internal/core/id"
	"metapus/internal/domain/posting"
)

const cryptoReceiptSuspenseKey = "crypto_receipt_suspense"

type CryptoReceiptSuspenseMovementSource interface {
	GenerateCryptoReceiptSuspenseMovements(context.Context) ([]crypto_receipt_suspense.CryptoReceiptSuspenseMovement, error)
}

type CryptoReceiptSuspenseVisitor struct{}

func (*CryptoReceiptSuspenseVisitor) Name() string { return cryptoReceiptSuspenseKey }

func (*CryptoReceiptSuspenseVisitor) CollectMovements(ctx context.Context, doc posting.Postable, set *posting.MovementSet) error {
	source, ok := doc.(CryptoReceiptSuspenseMovementSource)
	if !ok {
		return nil
	}
	movements, err := source.GenerateCryptoReceiptSuspenseMovements(ctx)
	if err != nil {
		return err
	}
	if len(movements) > 0 {
		set.SetExtension(cryptoReceiptSuspenseKey, movements)
	}
	return nil
}

type CryptoReceiptSuspenseRecorder struct {
	service *crypto_receipt_suspense.Service
}

func NewCryptoReceiptSuspenseRecorder(service *crypto_receipt_suspense.Service) *CryptoReceiptSuspenseRecorder {
	return &CryptoReceiptSuspenseRecorder{service: service}
}

func (*CryptoReceiptSuspenseRecorder) Name() string { return cryptoReceiptSuspenseKey }

func (r *CryptoReceiptSuspenseRecorder) RecordFromSet(ctx context.Context, set *posting.MovementSet) error {
	raw, ok := set.GetExtension(cryptoReceiptSuspenseKey)
	if !ok {
		return nil
	}
	movements, ok := raw.([]crypto_receipt_suspense.CryptoReceiptSuspenseMovement)
	if !ok {
		return fmt.Errorf("invalid receipt suspense movement set")
	}
	return r.service.RecordMovements(ctx, movements)
}

func (r *CryptoReceiptSuspenseRecorder) ReverseMovements(ctx context.Context, id id.ID, version int) error {
	return r.service.ReverseMovements(ctx, id, version)
}
