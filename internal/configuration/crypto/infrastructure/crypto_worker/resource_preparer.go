package crypto_worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/outbound_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/logger"
)

var ErrResourceRentalRequired = outboundapp.ErrResourceRentalRequired

// ResourcePreparer prepares blockchain resources before signing/broadcast.
// The only supported production path is shared resource rental.
type ResourcePreparer = outboundapp.ResourcePreparer

type ResourceBroadcastCommand = outbound_repo.ResourceBroadcastCommand

type ResourcePreparerResolver = outboundapp.ResourcePreparerResolver

type FamilyResourcePreparerRegistry struct {
	mu       sync.RWMutex
	byFamily map[crypto.ChainFamily]ResourcePreparer
}

func NewFamilyResourcePreparerRegistry() *FamilyResourcePreparerRegistry {
	return &FamilyResourcePreparerRegistry{byFamily: make(map[crypto.ChainFamily]ResourcePreparer)}
}

func (r *FamilyResourcePreparerRegistry) Register(family crypto.ChainFamily, preparer ResourcePreparer) error {
	if r == nil || !family.Valid() || preparer == nil {
		return fmt.Errorf("resource preparer with a valid family is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byFamily[family]; exists {
		return fmt.Errorf("resource preparer already registered: %s", family)
	}
	r.byFamily[family] = preparer
	return nil
}

func (r *FamilyResourcePreparerRegistry) Resolve(family crypto.ChainFamily) ResourcePreparer {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byFamily[family]
}

func (r *FamilyResourcePreparerRegistry) All() []ResourcePreparer {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	preparers := make([]ResourcePreparer, 0, len(r.byFamily))
	for _, preparer := range r.byFamily {
		preparers = append(preparers, preparer)
	}
	return preparers
}

func resolveResourcePreparer(resolver ResourcePreparerResolver, family crypto.ChainFamily) ResourcePreparer {
	if resolver == nil {
		return nil
	}
	return resolver.Resolve(family)
}

// SharedResourcePreparer prepares family resources through account-specific
// adapters. In-flight rentals stay with their original provider account.
type SharedResourcePreparer struct {
	log                    *logger.Logger
	sharedRepo             sharedResourceStore
	accountProviderFactory AccountProviderFactory
	accountProviders       sync.Map // account ID -> cachedAccountProvider
	targetEnergy           int64
	safetyMarginPct        int64
	reservationCutoff      time.Duration
	providerCircuits       sync.Map // network_id:provider -> *resourceProviderCircuit
}

func (p *SharedResourcePreparer) providerCircuit(networkID id.ID, provider string) *resourceProviderCircuit {
	key := networkID.String() + ":" + provider
	value, _ := p.providerCircuits.LoadOrStore(key, &resourceProviderCircuit{})
	return value.(*resourceProviderCircuit)
}

type cachedAccountProvider struct {
	version  int
	provider string
	adapter  crypto.ExternalResourceProvider
}

func (p *SharedResourcePreparer) ResourceProviderStatus(ctx context.Context, networkID id.ID) (bool, string) {
	return summarizeResourceProviderStatuses(p.ResourceProviderStatuses(ctx, networkID))
}

func summarizeResourceProviderStatuses(statuses []crypto.RuntimeEndpointStatus) (bool, string) {
	if len(statuses) == 0 {
		return false, "unconfigured"
	}
	ready := false
	state := "open"
	for _, status := range statuses {
		if status.State == "closed" {
			ready = true
			state = "closed"
			break
		}
		if status.State == "half_open" {
			state = "half_open"
		} else if status.State == "unavailable" && state == "open" {
			state = "unavailable"
		}
	}
	return ready, state
}

func (p *SharedResourcePreparer) ResourceProviderStatuses(ctx context.Context, networkID id.ID) []crypto.RuntimeEndpointStatus {
	if p == nil || p.sharedRepo == nil || p.accountProviderFactory == nil {
		return nil
	}
	accounts, err := p.sharedRepo.GetActiveNetworkProviderAccounts(ctx, networkID)
	if err != nil {
		if p.log != nil {
			p.log.Warnw("failed to list network resource provider accounts", "network_id", networkID, "error", err)
		}
		return []crypto.RuntimeEndpointStatus{{ProviderID: "catalog", Role: "resource", State: "unavailable"}}
	}
	now := time.Now().UTC()
	statuses := make([]crypto.RuntimeEndpointStatus, 0, len(accounts))
	for _, account := range accounts {
		circuit := p.providerCircuit(networkID, account.ID.String())
		state := circuit.state(now)
		if _, err := p.resolveProviderForAccount(ctx, account); err != nil {
			state = "unavailable"
		}
		statuses = append(statuses, crypto.RuntimeEndpointStatus{
			ProviderID: account.Code,
			Role:       "resource",
			State:      state,
			Failures:   circuit.failureCount(),
			LastError:  "",
		})
	}
	return statuses
}

type SharedResourcePreparerConfig struct {
	Store                  sharedResourceStore
	AccountProviderFactory AccountProviderFactory
	Logger                 *logger.Logger
	TargetEnergy           int64
	SafetyMarginPct        int64
	ReservationCutoff      time.Duration
}

func NewSharedResourcePreparer(cfg SharedResourcePreparerConfig) *SharedResourcePreparer {
	log := cfg.Logger
	if log == nil {
		log = logger.Default()
	}
	return &SharedResourcePreparer{
		log:                    log.WithComponent("resource_preparer"),
		sharedRepo:             cfg.Store,
		accountProviderFactory: cfg.AccountProviderFactory,
		targetEnergy:           cfg.TargetEnergy,
		safetyMarginPct:        cfg.SafetyMarginPct,
		reservationCutoff:      cfg.ReservationCutoff,
	}
}
