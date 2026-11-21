package crypto_worker

import (
	"context"
	"time"

	"metapus/internal/configuration/crypto/application/outboundapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
)

// This adapter exposes public account metadata and an opaque connection bound
// to the same account snapshot. Application never sees encrypted credentials.
type resourceRentalStore struct {
	sharedResourceStore
	runtime *SharedResourcePreparer
}

func (s resourceRentalStore) account(value *providerAccountRecord) *outboundapp.ResourceProviderAccount {
	if value == nil {
		return nil
	}
	return &outboundapp.ResourceProviderAccount{
		ID: value.ID, Provider: value.Provider, NetworkID: value.NetworkID, BalanceTokenID: value.BalanceTokenID,
		IsActive: value.IsActive, IsFolder: value.IsFolder, CredentialsConfigured: len(value.CredentialsEnc) > 0,
		Connection: resourceProviderConnection{runtime: s.runtime, account: value},
	}
}
func (s resourceRentalStore) GetProviderAccount(ctx context.Context, account id.ID) (*outboundapp.ResourceProviderAccount, error) {
	value, err := s.sharedResourceStore.GetProviderAccount(ctx, account)
	return s.account(value), err
}
func (s resourceRentalStore) GetProviderAccountForRental(ctx context.Context, account id.ID) (*outboundapp.ResourceProviderAccount, error) {
	value, err := s.sharedResourceStore.GetProviderAccountForRental(ctx, account)
	return s.account(value), err
}
func (s resourceRentalStore) GetActiveNetworkProviderAccounts(ctx context.Context, network id.ID) ([]*outboundapp.ResourceProviderAccount, error) {
	values, err := s.sharedResourceStore.GetActiveNetworkProviderAccounts(ctx, network)
	if err != nil {
		return nil, err
	}
	result := make([]*outboundapp.ResourceProviderAccount, 0, len(values))
	for _, value := range values {
		result = append(result, s.account(value))
	}
	return result, nil
}

type resourceProviderConnection struct {
	runtime *SharedResourcePreparer
	account *providerAccountRecord
}

func (c resourceProviderConnection) Resolve(ctx context.Context) (crypto.ExternalResourceProvider, error) {
	return c.runtime.resolveProviderForAccount(ctx, c.account)
}

type resourceCircuit struct{ circuit *resourceProviderCircuit }

func (c resourceCircuit) Allow(now time.Time) bool { return c.circuit.allow(now) }
func (c resourceCircuit) Success()                 { c.circuit.success() }
func (c resourceCircuit) Failure(now time.Time, threshold int, duration time.Duration) {
	c.circuit.failure(now, threshold, duration)
}
func (c resourceCircuit) CancelProbe() { c.circuit.cancelProbe() }
func (p *SharedResourcePreparer) Circuit(network id.ID, account string) outboundapp.ResourceProviderCircuit {
	return resourceCircuit{p.providerCircuit(network, account)}
}

func (p *SharedResourcePreparer) application() *outboundapp.ResourceRentalService {
	if p == nil || p.sharedRepo == nil {
		return nil
	}
	return outboundapp.NewResourceRentalService(outboundapp.ResourceRentalConfig{
		Store: resourceRentalStore{sharedResourceStore: p.sharedRepo, runtime: p}, Circuits: p, Logger: p.log,
		TargetEnergy: p.targetEnergy, SafetyMarginPct: p.safetyMarginPct, ReservationCutoff: p.reservationCutoff,
		MaintenanceBatchLimit: _outboundBatchLimit,
	})
}
func (p *SharedResourcePreparer) PrepareResource(ctx context.Context, request ResourceRequest) (ResourceReservationResult, error) {
	if p == nil || p.sharedRepo == nil || p.accountProviderFactory == nil {
		return ResourceReservationResult{}, nil
	}
	return p.application().PrepareResource(ctx, request)
}
func (p *SharedResourcePreparer) MarkResourceBroadcast(ctx context.Context, command ResourceBroadcastCommand) error {
	return p.application().MarkResourceBroadcast(ctx, command)
}
func (p *SharedResourcePreparer) SettleResource(ctx context.Context, hash string, receipt crypto.ChainReceipt) error {
	return p.application().SettleResource(ctx, hash, receipt)
}
func (p *SharedResourcePreparer) ReleaseResource(ctx context.Context, allocation id.ID, reason string) error {
	return p.application().ReleaseResource(ctx, allocation, reason)
}
func (p *SharedResourcePreparer) MaintainResources(ctx context.Context, worker string) error {
	if p == nil || p.sharedRepo == nil || p.accountProviderFactory == nil {
		return nil
	}
	return p.application().MaintainResources(ctx, worker)
}
