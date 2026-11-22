package cryptoapp

import (
	"context"
	"fmt"

	"metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/tenant"
)

type DepositAddressRotationService struct {
	repo      wallet.DepositRotationRepository
	publisher crypto.LifecycleWebhookPublisher
}

func NewDepositAddressRotationService(repo wallet.DepositRotationRepository, publisher crypto.LifecycleWebhookPublisher) *DepositAddressRotationService {
	if repo == nil || publisher == nil {
		panic("deposit address rotation requires repository and webhook publisher")
	}
	return &DepositAddressRotationService{repo: repo, publisher: publisher}
}

func (s *DepositAddressRotationService) Rotate(ctx context.Context, command wallet.DepositRotationCommand) (*wallet.DepositRotation, error) {
	if err := command.Validate(); err != nil {
		return nil, err
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return nil, err
	}
	var result *wallet.DepositRotation
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		var rotateErr error
		result, rotateErr = s.repo.RotatePersistentAddress(ctx, command)
		if rotateErr != nil || result.Replayed {
			return rotateErr
		}
		return s.publisher.PublishLifecycleWebhook(ctx, crypto.LifecycleWebhookEvent{
			Event: crypto.WebhookDepositAddressRotated, AggregateID: result.ID,
			MerchantID: result.MerchantID, ResourceVersion: result.Version,
			CausationKey: "deposit-address-rotation:" + result.ID.String(), OccurredAt: result.CreatedAt,
			Data: map[string]any{
				"id": result.ID.String(), "customerId": result.CustomerID,
				"networkId": result.NetworkID.String(), "oldWalletId": result.OldWalletID.String(),
				"newWalletId": result.NewWalletID.String(), "oldAddress": result.OldAddress,
				"newAddress": result.NewAddress, "reason": result.Reason,
				"resourceVersion": result.Version, "rotatedAt": result.CreatedAt,
			},
		})
	})
	if err != nil {
		return nil, fmt.Errorf("rotate deposit address: %w", err)
	}
	return result, nil
}
