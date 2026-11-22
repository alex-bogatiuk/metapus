package portalapp

import (
	"context"
	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/tx"
)

type AccessInvalidator interface {
	BumpUserAuthVersion(context.Context, id.ID, string) error
	InvalidateUserAuthCache(context.Context, id.ID)
}
type MerchantUserService struct {
	repo        merchant.MerchantUserRepository
	invalidator AccessInvalidator
	txm         tx.Manager
}

func NewMerchantUserService(repo merchant.MerchantUserRepository, invalidator AccessInvalidator, txm tx.Manager) *MerchantUserService {
	return &MerchantUserService{repo, invalidator, txm}
}
func (s *MerchantUserService) ListByMerchant(ctx context.Context, merchantID id.ID) ([]merchant.MerchantUser, error) {
	return s.repo.ListByMerchant(ctx, merchantID)
}
func (s *MerchantUserService) Add(ctx context.Context, userID, merchantID id.ID, role merchant.MerchantRole) error {
	if !role.IsValid() {
		return apperror.NewValidation("invalid role")
	}
	return s.mutate(ctx, userID, "merchant_access_changed", func(ctx context.Context) error { return s.repo.Add(ctx, userID, merchantID, role) })
}
func (s *MerchantUserService) Remove(ctx context.Context, userID, merchantID id.ID) error {
	return s.mutate(ctx, userID, "merchant_access_removed", func(ctx context.Context) error { return s.repo.Remove(ctx, userID, merchantID) })
}
func (s *MerchantUserService) UpdateRole(ctx context.Context, userID, merchantID id.ID, role merchant.MerchantRole) error {
	if !role.IsValid() {
		return apperror.NewValidation("invalid role")
	}
	return s.mutate(ctx, userID, "merchant_role_changed", func(ctx context.Context) error { return s.repo.UpdateRole(ctx, userID, merchantID, role) })
}
func (s *MerchantUserService) mutate(ctx context.Context, userID id.ID, reason string, fn func(context.Context) error) error {
	return s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := fn(ctx); err != nil {
			return err
		}
		if s.invalidator == nil {
			return nil
		}
		if err := s.invalidator.BumpUserAuthVersion(ctx, userID, reason); err != nil {
			return err
		}
		return tx.OnCommit(ctx, func(ctx context.Context) { s.invalidator.InvalidateUserAuthCache(ctx, userID) })
	})
}
