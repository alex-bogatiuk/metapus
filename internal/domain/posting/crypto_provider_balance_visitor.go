package posting

import (
	"context"
	"fmt"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/registers/crypto_provider_balance"
)

type CryptoProviderBalanceMovementSource interface {
	GenerateCryptoProviderBalanceMovements(ctx context.Context) ([]entity.CryptoProviderBalanceMovement, error)
}

const _cryptoProviderBalanceExtKey = "crypto_provider_balance"

type CryptoProviderBalanceVisitor struct{}

func (*CryptoProviderBalanceVisitor) Name() string { return _cryptoProviderBalanceExtKey }

func (*CryptoProviderBalanceVisitor) CollectMovements(ctx context.Context, doc Postable, set *MovementSet) error {
	src, ok := doc.(CryptoProviderBalanceMovementSource)
	if !ok {
		return nil
	}
	movements, err := src.GenerateCryptoProviderBalanceMovements(ctx)
	if err != nil {
		return fmt.Errorf("generate crypto provider balance movements: %w", err)
	}
	if len(movements) > 0 {
		set.SetExtension(_cryptoProviderBalanceExtKey, movements)
	}
	return nil
}

type CryptoProviderBalanceRecorder struct {
	service *crypto_provider_balance.Service
}

func NewCryptoProviderBalanceRecorder(service *crypto_provider_balance.Service) *CryptoProviderBalanceRecorder {
	return &CryptoProviderBalanceRecorder{service: service}
}

func (*CryptoProviderBalanceRecorder) Name() string { return _cryptoProviderBalanceExtKey }

func (r *CryptoProviderBalanceRecorder) RecordFromSet(ctx context.Context, set *MovementSet) error {
	raw, ok := set.GetExtension(_cryptoProviderBalanceExtKey)
	if !ok {
		return nil
	}
	movements, ok := raw.([]entity.CryptoProviderBalanceMovement)
	if !ok {
		return fmt.Errorf("invalid crypto provider balance movement set")
	}
	return r.service.RecordMovements(ctx, movements)
}

func (r *CryptoProviderBalanceRecorder) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.service.ReverseMovements(ctx, recorderID, beforeVersion)
}
