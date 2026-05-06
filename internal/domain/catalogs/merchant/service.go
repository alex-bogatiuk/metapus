package merchant

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// Repository defines storage operations for Merchant.
type Repository interface {
	domain.CatalogRepository[*Merchant]

	// GetMerchantIDsByUserID returns all merchant IDs accessible by a user.
	GetMerchantIDsByUserID(ctx context.Context, userID id.ID) ([]id.ID, error)

	// GetUsersByMerchantID returns all users associated with a merchant.
	GetUsersByMerchantID(ctx context.Context, merchantID id.ID) ([]MerchantUser, error)

	// AddUser creates a user-merchant association.
	AddUser(ctx context.Context, merchantID, userID id.ID, role MerchantRole) error

	// RemoveUser deletes a user-merchant association.
	RemoveUser(ctx context.Context, merchantID, userID id.ID) error
}

// Service provides business logic for Merchant catalog.
type Service struct {
	*domain.CatalogService[*Merchant]
	repo Repository
}

// NewService creates a new Merchant service.
func NewService(repo Repository, num numerator.Generator) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*Merchant]{
		Repo:       repo,
		Numerator:  num,
		EntityName: "merchant",
	})

	svc := &Service{
		CatalogService: base,
		repo:           repo,
	}

	return svc
}

// GetMerchantIDsByUserID returns all merchant IDs accessible by a user.
// Used by DataScope resolution: UserID → sys_merchant_users → []MerchantID.
func (s *Service) GetMerchantIDsByUserID(ctx context.Context, userID id.ID) ([]id.ID, error) {
	return s.repo.GetMerchantIDsByUserID(ctx, userID)
}

// GetUsersByMerchantID returns all users associated with a merchant.
func (s *Service) GetUsersByMerchantID(ctx context.Context, merchantID id.ID) ([]MerchantUser, error) {
	return s.repo.GetUsersByMerchantID(ctx, merchantID)
}

// AddUser creates a user-merchant association with the specified role.
func (s *Service) AddUser(ctx context.Context, merchantID, userID id.ID, role MerchantRole) error {
	return s.repo.AddUser(ctx, merchantID, userID, role)
}

// RemoveUser deletes a user-merchant association.
func (s *Service) RemoveUser(ctx context.Context, merchantID, userID id.ID) error {
	return s.repo.RemoveUser(ctx, merchantID, userID)
}
