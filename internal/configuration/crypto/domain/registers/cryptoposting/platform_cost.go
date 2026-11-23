package cryptoposting

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/registers/crypto_platform_cost"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/posting"
)

// CryptoPlatformCostMovementSource is implemented by documents that generate
// platform-owned crypto cost movements.
type CryptoPlatformCostMovementSource interface {
	GenerateCryptoPlatformCostMovements(ctx context.Context) ([]crypto_platform_cost.CryptoPlatformCostMovement, error)
}

const _cryptoPlatformCostExtKey = "crypto_platform_cost"

// CryptoPlatformCostVisitor collects platform cost movements from documents.
type CryptoPlatformCostVisitor struct{}

func (v *CryptoPlatformCostVisitor) Name() string { return _cryptoPlatformCostExtKey }

func (v *CryptoPlatformCostVisitor) CollectMovements(ctx context.Context, doc posting.Postable, set *posting.MovementSet) error {
	src, ok := doc.(CryptoPlatformCostMovementSource)
	if !ok {
		return nil
	}
	movements, err := src.GenerateCryptoPlatformCostMovements(ctx)
	if err != nil {
		return fmt.Errorf("generate crypto platform cost movements: %w", err)
	}
	if len(movements) > 0 {
		set.SetExtension(_cryptoPlatformCostExtKey, movements)
	}
	return nil
}

// CryptoPlatformCostRecorder adapts crypto_platform_cost.Service into a posting.RegisterRecorder.
type CryptoPlatformCostRecorder struct {
	service *crypto_platform_cost.Service
}

func NewCryptoPlatformCostRecorder(s *crypto_platform_cost.Service) *CryptoPlatformCostRecorder {
	return &CryptoPlatformCostRecorder{service: s}
}

func (r *CryptoPlatformCostRecorder) Name() string { return _cryptoPlatformCostExtKey }

func (r *CryptoPlatformCostRecorder) RecordFromSet(ctx context.Context, set *posting.MovementSet) error {
	raw, ok := set.GetExtension(_cryptoPlatformCostExtKey)
	if !ok {
		return nil
	}
	movements, ok := raw.([]crypto_platform_cost.CryptoPlatformCostMovement)
	if !ok || len(movements) == 0 {
		return nil
	}
	return r.service.RecordMovements(ctx, movements)
}

func (r *CryptoPlatformCostRecorder) ReverseMovements(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return r.service.ReverseMovements(ctx, recorderID, beforeVersion)
}
