package crypto_worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"metapus/internal/application/outboundapp"
	corecrypto "metapus/internal/core/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres/outbound_repo"
	"metapus/internal/platform/secretfile"
)

const (
	resourceTypeEnergy = "energy"
)

var ErrResourceCapacityInsufficient = outbound_repo.ErrResourceCapacityInsufficient
var ErrResourceReclaimPending = outbound_repo.ErrResourceReclaimPending
var ErrResourceRentalExists = outbound_repo.ErrResourceRentalExists

type ResourceRequest = outbound_repo.ResourceRequest

type ResourceReservationResult = outboundapp.ResourceReservationResult

type providerAccountRecord = outbound_repo.ProviderAccountRecord

type AccountProviderFactory func(ctx context.Context, providerCode string, credsJSON []byte) (crypto.ExternalResourceProvider, error)

type resourceRentalRecord = outbound_repo.ResourceRentalRecord

type resourceAllocationRecord = outbound_repo.ResourceAllocationRecord

type sharedResourceStore interface {
	GetProviderAccount(ctx context.Context, accountID id.ID) (*providerAccountRecord, error)
	GetProviderAccountForRental(ctx context.Context, accountID id.ID) (*providerAccountRecord, error)
	GetActiveNetworkProviderAccounts(ctx context.Context, networkID id.ID) ([]*providerAccountRecord, error)
	GetResourceAllocationByBasis(ctx context.Context, basisType string, basisID id.ID) (*resourceAllocationRecord, error)
	GetOpenWalletRental(ctx context.Context, networkID, walletID id.ID) (*resourceRentalRecord, error)
	TryReserveResource(ctx context.Context, req ResourceRequest, required int64, cutoff time.Duration) (*resourceAllocationRecord, *resourceRentalRecord, error)
	CreateResourceRental(ctx context.Context, rental *resourceRentalRecord, req ResourceRequest, requestJSON []byte) error
	ActivateResourceRental(ctx context.Context, rentalID id.ID, serial string, delegated int64, cost types.CryptoAmount, providerBalance *types.CryptoAmount, responseJSON []byte, expiresAt time.Time) error
	MarkResourceRentalHeld(ctx context.Context, rentalID id.ID, reason string, responseJSON []byte) error
	MarkResourceRentalReclaiming(ctx context.Context, rentalID id.ID) error
	CloseResourceRental(ctx context.Context, rentalID id.ID, refund types.CryptoAmount, providerBalance *types.CryptoAmount, responseJSON []byte, status string) error
	MarkResourceAllocationBroadcast(ctx context.Context, command ResourceBroadcastCommand) error
	SettleResourceAllocation(ctx context.Context, txHash string, receipt crypto.ChainReceipt) error
	ReleaseResourceAllocation(ctx context.Context, allocationID id.ID, reason string) error
	ClaimResourceRentalsForMaintenance(ctx context.Context, workerID string, limit int) ([]*resourceRentalRecord, error)
	RenewResourceRentalClaim(ctx context.Context, claim outboundapp.ResourceRentalClaim) error
	MarkResourceRentalClaimReclaiming(ctx context.Context, claim outboundapp.ResourceRentalClaim) error
	MarkResourceRentalClaimHeld(ctx context.Context, claim outboundapp.ResourceRentalClaim, reason string, responseJSON []byte) error
	CloseResourceRentalClaim(ctx context.Context, claim outboundapp.ResourceRentalClaim, refund types.CryptoAmount, providerBalance *types.CryptoAmount, responseJSON []byte, status string) error
	ReleaseResourceRentalClaim(ctx context.Context, claim outboundapp.ResourceRentalClaim) error
}

func (p *SharedResourcePreparer) resolveProviderForAccount(ctx context.Context, account *providerAccountRecord) (crypto.ExternalResourceProvider, error) {
	if account == nil {
		return nil, fmt.Errorf("provider account is nil")
	}
	if len(account.CredentialsEnc) > 0 && p.accountProviderFactory != nil {
		cacheKey := account.ID.String()
		if cached, ok := p.accountProviders.Load(cacheKey); ok {
			entry := cached.(*cachedAccountProvider)
			if entry.version > account.CredentialsVersion ||
				(entry.version == account.CredentialsVersion && entry.provider == account.Provider) {
				return entry.adapter, nil
			}
		}
		decJSON, err := decryptProviderCredentials(account.CredentialsEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypt credentials for account %s: %w", account.ID, err)
		}
		adapter, err := p.accountProviderFactory(ctx, account.Provider, decJSON)
		if err != nil {
			return nil, fmt.Errorf("build adapter for account %s (%s): %w", account.ID, account.Provider, err)
		}
		if adapter != nil {
			newEntry := &cachedAccountProvider{version: account.CredentialsVersion, provider: account.Provider, adapter: adapter}
			for {
				cached, exists := p.accountProviders.Load(cacheKey)
				if !exists {
					if _, loaded := p.accountProviders.LoadOrStore(cacheKey, newEntry); !loaded {
						p.providerCircuit(account.NetworkID, cacheKey).success()
						return adapter, nil
					}
					continue
				}
				entry := cached.(*cachedAccountProvider)
				if entry.version > account.CredentialsVersion ||
					(entry.version == account.CredentialsVersion && entry.provider == account.Provider) {
					return entry.adapter, nil
				}
				if p.accountProviders.CompareAndSwap(cacheKey, entry, newEntry) {
					p.providerCircuit(account.NetworkID, cacheKey).success()
					return adapter, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("%w: %s", crypto.ErrResourceProviderNotFound, account.Provider)
}

func decryptProviderCredentials(enc []byte) ([]byte, error) {
	if len(enc) == 0 {
		return nil, nil
	}
	keyStr, err := secretfile.Get("AUTOMATION_ENCRYPTION_KEY", "")
	if err != nil {
		return nil, fmt.Errorf("load encryption key: %w", err)
	}
	if len(keyStr) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(keyStr))
	}
	return corecrypto.Decrypt(enc, []byte(keyStr))
}

type resourceProviderCircuit struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
	halfOpen  bool
}

func (c *resourceProviderCircuit) allow(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.openUntil.IsZero() {
		return true
	}
	if now.Before(c.openUntil) || c.halfOpen {
		return false
	}
	c.halfOpen = true
	return true
}

func (c *resourceProviderCircuit) success() {
	c.mu.Lock()
	c.failures, c.openUntil, c.halfOpen = 0, time.Time{}, false
	c.mu.Unlock()
}

func (c *resourceProviderCircuit) failure(now time.Time, threshold int, openFor time.Duration) {
	if threshold <= 0 {
		threshold = 3
	}
	if openFor <= 0 {
		openFor = 30 * time.Second
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.halfOpen = false
	c.failures++
	if c.failures >= threshold {
		c.openUntil = now.Add(openFor)
	}
}

func (c *resourceProviderCircuit) cancelProbe() {
	c.mu.Lock()
	c.halfOpen = false
	c.mu.Unlock()
}

func (c *resourceProviderCircuit) state(now time.Time) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.openUntil.IsZero() && now.Before(c.openUntil) {
		return "open"
	}
	if c.halfOpen {
		return "half_open"
	}
	return "closed"
}

func (c *resourceProviderCircuit) failureCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failures
}
