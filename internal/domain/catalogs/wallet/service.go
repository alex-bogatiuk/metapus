package wallet

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
)

// Repository defines storage operations for Wallet.
type Repository interface {
	domain.CatalogRepository[*Wallet]

	// LeaseForInvoice atomically leases a free pool wallet for an invoice.
	// Uses SELECT ... FOR UPDATE SKIP LOCKED for contention-free allocation.
	LeaseForInvoice(ctx context.Context, invoiceID, networkID id.ID) (*Wallet, error)

	// FindByAddress retrieves a wallet by blockchain address and network.
	FindByAddress(ctx context.Context, networkID id.ID, address string) (*Wallet, error)

	// CountFreeByNetwork returns the number of free pool wallets for a network.
	CountFreeByNetwork(ctx context.Context, networkID id.ID) (int, error)

	// FindPersistentByCustomerRef finds an existing persistent wallet for a customer.
	FindPersistentByCustomerRef(ctx context.Context, merchantID, networkID id.ID, customerRef string) (*Wallet, error)

	// AssignPersistentAddress atomically assigns a free pool wallet to a customer persistently.
	AssignPersistentAddress(ctx context.Context, merchantID, networkID id.ID, customerRef string) (*Wallet, error)
}

// PoolStats holds wallet pool statistics for a blockchain network.
type PoolStats struct {
	NetworkID    id.ID `json:"networkId"`
	Total        int   `json:"total"`
	Free         int   `json:"free"`
	Leased       int   `json:"leased"`
	SweepPending int   `json:"sweepPending"`
	Frozen       int   `json:"frozen"`
}

// Service provides business logic for Wallet catalog.
type Service struct {
	*domain.CatalogService[*Wallet]
	repo Repository
}

// NewService creates a new Wallet service.
func NewService(repo Repository, num numerator.Generator) *Service {
	base := domain.NewCatalogService(domain.CatalogServiceConfig[*Wallet]{
		Repo:       repo,
		Numerator:  num,
		EntityName: "wallet",
	})

	svc := &Service{
		CatalogService: base,
		repo:           repo,
	}

	return svc
}

// LeaseForInvoice atomically leases a free pool wallet for an invoice.
// Returns apperror.NotFound if no free wallets are available.
func (s *Service) LeaseForInvoice(ctx context.Context, invoiceID, networkID id.ID) (*Wallet, error) {
	w, err := s.repo.LeaseForInvoice(ctx, invoiceID, networkID)
	if err != nil {
		return nil, fmt.Errorf("lease wallet for invoice %s: %w", invoiceID, err)
	}
	return w, nil
}

// ReleaseWallet returns a wallet to the free pool.
func (s *Service) ReleaseWallet(ctx context.Context, walletID id.ID) error {
	w, err := s.repo.GetByID(ctx, walletID)
	if err != nil {
		return fmt.Errorf("get wallet %s: %w", walletID, err)
	}

	if w.Status == WalletStatusFree {
		return apperror.NewValidation("wallet is already free").
			WithDetail("walletId", walletID.String())
	}

	w.Release()
	if err := s.repo.Update(ctx, w); err != nil {
		return fmt.Errorf("release wallet %s: %w", walletID, err)
	}
	return nil
}

// MarkSweepPending marks a wallet as pending sweep after invoice confirmation.
func (s *Service) MarkSweepPending(ctx context.Context, walletID id.ID) error {
	w, err := s.repo.GetByID(ctx, walletID)
	if err != nil {
		return fmt.Errorf("get wallet %s: %w", walletID, err)
	}

	w.MarkSweepPending()
	if err := s.repo.Update(ctx, w); err != nil {
		return fmt.Errorf("mark sweep pending %s: %w", walletID, err)
	}
	return nil
}

// FindByAddress retrieves a wallet by blockchain address and network.
func (s *Service) FindByAddress(ctx context.Context, networkID id.ID, address string) (*Wallet, error) {
	return s.repo.FindByAddress(ctx, networkID, address)
}

// CountFreeByNetwork returns available pool wallets count for a network.
func (s *Service) CountFreeByNetwork(ctx context.Context, networkID id.ID) (int, error) {
	return s.repo.CountFreeByNetwork(ctx, networkID)
}

// AssignPersistentAddress assigns a permanent address to a customer (or returns existing).
func (s *Service) AssignPersistentAddress(ctx context.Context, merchantID, networkID id.ID, customerRef string) (*Wallet, error) {
	// 1. Check idempotency
	existing, err := s.repo.FindPersistentByCustomerRef(ctx, merchantID, networkID, customerRef)
	if err == nil && existing != nil {
		return existing, nil
	}

	// 2. Assign new
	w, err := s.repo.AssignPersistentAddress(ctx, merchantID, networkID, customerRef)
	if err != nil {
		return nil, fmt.Errorf("assign persistent address for customer %s: %w", customerRef, err)
	}
	return w, nil
}
