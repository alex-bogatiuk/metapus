package outboundapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/logger"
)

const _resourceTypeEnergy = string(crypto.ResourceKindEnergy)

type ResourceRentalConfig struct {
	Store                 ResourceRentalRepository
	Circuits              ResourceCircuitRegistry
	Logger                *logger.Logger
	TargetEnergy          int64
	SafetyMarginPct       int64
	ReservationCutoff     time.Duration
	MaintenanceBatchLimit int
	Now                   func() time.Time
}

type ResourceRentalService struct {
	sharedRepo            ResourceRentalRepository
	circuits              ResourceCircuitRegistry
	log                   *logger.Logger
	targetEnergy          int64
	safetyMarginPct       int64
	reservationCutoff     time.Duration
	maintenanceBatchLimit int
	now                   func() time.Time
}

func NewResourceRentalService(c ResourceRentalConfig) *ResourceRentalService {
	if c.TargetEnergy <= 0 {
		c.TargetEnergy = 65_000
	}
	if c.SafetyMarginPct <= 0 {
		c.SafetyMarginPct = 10
	}
	if c.ReservationCutoff <= 0 {
		c.ReservationCutoff = 2 * time.Minute
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.MaintenanceBatchLimit <= 0 {
		c.MaintenanceBatchLimit = 20
	}
	return &ResourceRentalService{
		sharedRepo: c.Store, circuits: c.Circuits, log: c.Logger,
		targetEnergy: c.TargetEnergy, safetyMarginPct: c.SafetyMarginPct,
		reservationCutoff: c.ReservationCutoff, maintenanceBatchLimit: c.MaintenanceBatchLimit, now: c.Now,
	}
}

func (p *ResourceRentalService) PrepareResource(ctx context.Context, req ResourceRequest) (ResourceReservationResult, error) {
	if p == nil || p.sharedRepo == nil {
		return ResourceReservationResult{}, nil
	}
	if id.IsNil(req.BasisID) || id.IsNil(req.NetworkID) || id.IsNil(req.SourceWalletID) {
		return ResourceReservationResult{}, fmt.Errorf("resource request basis, network, and source wallet are required")
	}
	if existing, err := p.sharedRepo.GetResourceAllocationByBasis(ctx, req.BasisType, req.BasisID); err != nil {
		return ResourceReservationResult{}, err
	} else if existing != nil {
		return ResourceReservationResult{AllocationID: existing.ID, RentalID: existing.RentalID,
			ReservedUnits: existing.ReservedAmount, ResourceUsed: true}, nil
	}
	requiredUnits := resourceRequirementQuantity(req, _resourceTypeEnergy)
	if requiredUnits <= 0 {
		return ResourceReservationResult{}, fmt.Errorf("resource quote has no positive %s units", _resourceTypeEnergy)
	}
	required := addEnergySafetyMargin(requiredUnits, p.safetyMarginPct)

	allocation, rental, err := p.sharedRepo.TryReserveResource(ctx, req, required, p.reservationCutoff)
	if err == nil && allocation != nil {
		return ResourceReservationResult{AllocationID: allocation.ID, RentalID: allocation.RentalID,
			ReservedUnits: allocation.ReservedAmount, ResourceUsed: true}, nil
	}
	if err != nil && !errors.Is(err, ErrResourceCapacityInsufficient) {
		return ResourceReservationResult{}, err
	}

	if rental != nil {
		if rental.ExternalSerial == "" && (rental.Status == "created" || rental.Status == "ordering" || rental.Status == "held") {
			recovered, recoverErr := p.recoverPendingRental(ctx, rental, req)
			if recoverErr != nil {
				return ResourceReservationResult{}, recoverErr
			}
			if !recovered {
				return ResourceReservationResult{}, ErrResourceReclaimPending
			}
			allocation, _, err = p.sharedRepo.TryReserveResource(ctx, req, required, p.reservationCutoff)
			if err != nil {
				return ResourceReservationResult{}, err
			}
			if allocation == nil {
				return ResourceReservationResult{}, ErrResourceCapacityInsufficient
			}
			return ResourceReservationResult{AllocationID: allocation.ID, RentalID: allocation.RentalID,
				ReservedUnits: allocation.ReservedAmount, ResourceUsed: true}, nil
		}
		if rental.Status == "reclaiming" {
			provider, resolveErr := p.resolveProviderForRental(ctx, rental)
			if resolveErr != nil {
				return ResourceReservationResult{}, resolveErr
			}
			closed, recoverErr := p.recoverReclaimingRental(ctx, rental, provider)
			if recoverErr != nil {
				return ResourceReservationResult{}, recoverErr
			}
			if !closed {
				return ResourceReservationResult{}, ErrResourceReclaimPending
			}
		} else {
			if rental.ExternalSerial == "" {
				return ResourceReservationResult{}, ErrResourceCapacityInsufficient
			}
			provider, resolveErr := p.resolveProviderForRental(ctx, rental)
			if resolveErr != nil {
				return ResourceReservationResult{}, resolveErr
			}
			if err := p.sharedRepo.MarkResourceRentalReclaiming(ctx, rental.ID); err != nil {
				return ResourceReservationResult{}, err
			}
			if _, err := provider.Reclaim(ctx, crypto.ResourceOrderRef{ExternalID: rental.ExternalSerial, ClientOrderID: rental.OutTradeNo}); err != nil {
				_ = p.sharedRepo.MarkResourceRentalHeld(ctx, rental.ID, err.Error(), nil)
				return ResourceReservationResult{}, err
			}
			return ResourceReservationResult{}, ErrResourceReclaimPending
		}
	}

	return p.placeNewRental(ctx, req, required)
}

func (p *ResourceRentalService) placeNewRental(ctx context.Context, req ResourceRequest, required int64) (ResourceReservationResult, error) {
	if p.circuits == nil {
		return ResourceReservationResult{}, fmt.Errorf("resource provider circuits are not configured")
	}
	var lastTransient error
	accountIDs := req.ProviderAccountIDs
	if len(accountIDs) == 0 {
		accounts, err := p.sharedRepo.GetActiveNetworkProviderAccounts(ctx, req.NetworkID)
		if err != nil {
			return ResourceReservationResult{}, err
		}
		accountIDs = make([]id.ID, 0, len(accounts))
		for _, account := range accounts {
			accountIDs = append(accountIDs, account.ID)
		}
	}
	if len(accountIDs) == 0 {
		return ResourceReservationResult{}, fmt.Errorf("%w: no configured resource provider accounts", crypto.ErrResourceProviderTransient)
	}
	for _, accountID := range accountIDs {
		circuit := p.circuits.Circuit(req.NetworkID, accountID.String())
		account, err := p.sharedRepo.GetProviderAccount(ctx, accountID)
		if err != nil {
			return ResourceReservationResult{}, err
		}
		if account == nil || !account.IsActive || account.IsFolder || account.NetworkID != req.NetworkID || !account.CredentialsConfigured {
			continue
		}
		if account.Connection == nil {
			continue
		}
		provider, err := account.Connection.Resolve(ctx)
		if err != nil {
			continue
		}
		if !circuit.Allow(p.now().UTC()) {
			lastTransient = fmt.Errorf("%w: account %s circuit is open", crypto.ErrResourceProviderTransient, accountID)
			continue
		}
		result, placeErr := p.placeWithAccountAndProvider(ctx, req, required, account.ID, account.BalanceTokenID, provider, circuit)
		if placeErr == nil {
			return result, nil
		}
		if errors.Is(placeErr, crypto.ErrResourceOrderAmbiguous) {
			return ResourceReservationResult{}, placeErr
		}
		if errors.Is(placeErr, crypto.ErrResourceProviderTransient) ||
			errors.Is(placeErr, crypto.ErrResourceProviderUnauthorized) ||
			errors.Is(placeErr, crypto.ErrResourceProviderInvalidReply) {
			lastTransient = placeErr
			continue
		}
		return ResourceReservationResult{}, placeErr
	}
	if lastTransient != nil {
		return ResourceReservationResult{}, lastTransient
	}
	return ResourceReservationResult{}, fmt.Errorf("%w: no healthy resource provider account", crypto.ErrResourceProviderTransient)
}

func (p *ResourceRentalService) placeWithAccountAndProvider(
	ctx context.Context,
	req ResourceRequest,
	required int64,
	providerAccountID id.ID,
	costTokenID id.ID,
	provider crypto.ExternalResourceProvider,
	circuit ResourceProviderCircuit,
) (ResourceReservationResult, error) {
	capabilities := provider.Capabilities()
	rentalID := id.New()
	outTradeNo := makeRentalOutTradeNo(req.TenantID, rentalID)
	capacity := max(required, p.targetEnergy, capabilities.MinQuantity)
	expiresAt := p.now().UTC().Add(capabilities.DefaultDuration)
	intent := crypto.ResourceOrderIntent{
		ClientOrderID:  outTradeNo,
		ResourceKind:   crypto.ResourceKindEnergy,
		Quantity:       capacity,
		ReceiveAddress: req.SourceAddress,
		Period:         capabilities.DefaultPeriod,
	}
	requestJSON, _ := json.Marshal(intent)
	rental := &ResourceRental{
		ID: rentalID, ProviderAccountID: providerAccountID, NetworkID: req.NetworkID,
		ReceiveWalletID: req.SourceWalletID, CostTokenID: costTokenID,
		RequestedAmount: capacity, Provider: provider.Code(), OutTradeNo: outTradeNo,
		Status: "ordering", ExpiresAt: &expiresAt,
	}
	if err := p.sharedRepo.CreateResourceRental(ctx, rental, req, requestJSON); err != nil {
		circuit.CancelProbe()
		if errors.Is(err, ErrResourceRentalExists) {
			return ResourceReservationResult{}, ErrResourceReclaimPending
		}
		return ResourceReservationResult{}, err
	}

	order, placeErr := provider.Place(ctx, intent)
	p.recordProviderResult(circuit, placeErr, req)
	if placeErr != nil {
		query, queryErr := provider.Query(ctx, crypto.ResourceOrderRef{ClientOrderID: outTradeNo})
		if queryErr == nil && query.ExternalID != "" {
			circuit.Success()
			order = query
			placeErr = nil
		} else {
			if queryErr != nil {
				p.recordProviderResult(circuit, queryErr, req)
			}
			_ = p.sharedRepo.MarkResourceRentalHeld(ctx, rental.ID, placeErr.Error(), order.ResponseJSON)
			return ResourceReservationResult{}, fmt.Errorf("%w: place=%v query=%v", crypto.ErrResourceOrderAmbiguous, placeErr, queryErr)
		}
	}
	if err := p.activateFromOrder(ctx, rental, order, expiresAt); err != nil {
		return ResourceReservationResult{}, err
	}
	if order.Status.Terminal() {
		return ResourceReservationResult{}, fmt.Errorf("resource rental is terminal with status %s after charging %s", order.Status, order.CostAmount)
	}

	allocation, _, err := p.sharedRepo.TryReserveResource(ctx, req, required, p.reservationCutoff)
	if err != nil {
		return ResourceReservationResult{}, err
	}
	if allocation == nil {
		return ResourceReservationResult{}, fmt.Errorf("resource rental activated without reservable capacity")
	}
	if p.log != nil {
		p.log.Infow("shared resource rental reserved", "rental_id", rental.ID, "allocation_id", allocation.ID,
			"provider", provider.Code(), "account_id", providerAccountID, "source", req.SourceAddress,
			"requested_energy", capacity, "reserved_energy", required, "serial", order.ExternalID, "cost_amount", order.CostAmount)
	}
	return ResourceReservationResult{AllocationID: allocation.ID, RentalID: rental.ID,
		ReservedUnits: allocation.ReservedAmount, ResourceUsed: true}, nil
}

func (p *ResourceRentalService) resolveProviderForRental(ctx context.Context, rental *ResourceRental) (crypto.ExternalResourceProvider, error) {
	if rental == nil || id.IsNil(rental.ProviderAccountID) {
		return nil, fmt.Errorf("resource rental has no provider account")
	}
	account, err := p.sharedRepo.GetProviderAccountForRental(ctx, rental.ProviderAccountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.NetworkID != rental.NetworkID || account.Provider != rental.Provider {
		return nil, fmt.Errorf("%w: account %s", crypto.ErrResourceProviderNotFound, rental.ProviderAccountID)
	}
	if account.Connection == nil {
		return nil, fmt.Errorf("%w: account %s has no connection", crypto.ErrResourceProviderNotFound, rental.ProviderAccountID)
	}
	return account.Connection.Resolve(ctx)
}

func (p *ResourceRentalService) recordProviderResult(circuit ResourceProviderCircuit, err error, req ResourceRequest) {
	if err == nil {
		circuit.Success()
		return
	}
	if errors.Is(err, crypto.ErrResourceProviderTransient) {
		circuit.Failure(p.now().UTC(), req.ProviderFailureThreshold, req.ProviderOpenDuration)
		return
	}
	if errors.Is(err, crypto.ErrResourceProviderUnauthorized) {
		circuit.Failure(p.now().UTC(), 1, req.ProviderOpenDuration)
		return
	}
	if errors.Is(err, crypto.ErrResourceProviderInvalidReply) {
		circuit.Failure(p.now().UTC(), req.ProviderFailureThreshold, req.ProviderOpenDuration)
		return
	}
	circuit.CancelProbe()
}

func (p *ResourceRentalService) activateFromOrder(ctx context.Context, rental *ResourceRental, order crypto.ResourceOrder, expiresAt time.Time) error {
	delegated := order.DelegatedAmount
	if delegated <= 0 {
		delegated = rental.RequestedAmount
	}
	if err := p.sharedRepo.ActivateResourceRental(ctx, rental.ID, order.ExternalID, delegated, order.CostAmount,
		order.ProviderBalance, order.ResponseJSON, expiresAt); err != nil {
		return err
	}
	if !order.Status.Terminal() {
		return nil
	}
	status := "closed"
	if order.Status == crypto.ResourceOrderFailed {
		status = "failed"
	}
	return p.sharedRepo.CloseResourceRental(ctx, rental.ID, order.RefundAmount, order.ProviderBalance, order.ResponseJSON, status)
}

// resourceOrderAbsenceGrace is how long a provider may take to make a placed
// order queryable; a "not found" reply inside it stays ambiguous.
const resourceOrderAbsenceGrace = 5 * time.Minute

func (p *ResourceRentalService) orderAbsenceIsSettled(rental *ResourceRental, err error) bool {
	if !errors.Is(err, crypto.ErrResourceOrderNotFound) || rental.ExternalSerial != "" || rental.CreatedAt.IsZero() {
		return false
	}
	return p.now().UTC().Sub(rental.CreatedAt.UTC()) >= resourceOrderAbsenceGrace
}

func (p *ResourceRentalService) recoverPendingRental(ctx context.Context, rental *ResourceRental, req ResourceRequest) (bool, error) {
	provider, err := p.resolveProviderForRental(ctx, rental)
	if err != nil {
		_ = p.sharedRepo.MarkResourceRentalHeld(ctx, rental.ID, err.Error(), nil)
		return false, fmt.Errorf("%w: recover rental %s: %v", crypto.ErrResourceOrderAmbiguous, rental.OutTradeNo, err)
	}
	order, queryErr := provider.Query(ctx, crypto.ResourceOrderRef{ClientOrderID: rental.OutTradeNo, ExternalID: rental.ExternalSerial})
	if queryErr != nil && p.orderAbsenceIsSettled(rental, queryErr) {
		// The provider explicitly has no order for this client ID long after the
		// attempt: the original placement never reached it. Re-placing with the
		// same out_trade_no below is idempotent at the provider.
		order, queryErr = crypto.ResourceOrder{}, nil
	}
	if queryErr != nil {
		_ = p.sharedRepo.MarkResourceRentalHeld(ctx, rental.ID, queryErr.Error(), nil)
		return false, fmt.Errorf("%w: recover rental %s: %v", crypto.ErrResourceOrderAmbiguous, rental.OutTradeNo, queryErr)
	}
	replaced := order.ExternalID == ""
	if replaced {
		receiveAddress := req.SourceAddress
		if receiveAddress == "" {
			receiveAddress = rental.ReceiveAddress
		}
		capabilities := provider.Capabilities()
		intent := crypto.ResourceOrderIntent{
			ClientOrderID:  rental.OutTradeNo,
			ResourceKind:   crypto.ResourceKindEnergy,
			Quantity:       rental.RequestedAmount,
			ReceiveAddress: receiveAddress,
			Period:         capabilities.DefaultPeriod,
		}
		placed, placeErr := provider.Place(ctx, intent)
		if placeErr != nil {
			_ = p.sharedRepo.MarkResourceRentalHeld(ctx, rental.ID, placeErr.Error(), placed.ResponseJSON)
			return false, placeErr
		}
		order = placed
	}
	expiresAt := p.now().UTC().Add(provider.Capabilities().DefaultDuration)
	// A recovered order keeps its original expiry; an order placed only now
	// lives for a full period from now. Reusing the original expiry made a
	// fresh delegation look expired, and maintenance reclaimed it at once.
	if rental.ExpiresAt != nil && !replaced {
		expiresAt = *rental.ExpiresAt
	}
	if err := p.activateFromOrder(ctx, rental, order, expiresAt); err != nil {
		return false, err
	}
	if order.Status.Terminal() {
		return false, fmt.Errorf("resource rental is terminal with status %s after charging %s", order.Status, order.CostAmount)
	}
	return true, nil
}

func (p *ResourceRentalService) MarkResourceBroadcast(ctx context.Context, command ResourceBroadcastCommand) error {
	if p == nil || p.sharedRepo == nil {
		return nil
	}
	return p.sharedRepo.MarkResourceAllocationBroadcast(ctx, command)
}

func (p *ResourceRentalService) SettleResource(ctx context.Context, txHash string, receipt crypto.ChainReceipt) error {
	if p == nil || p.sharedRepo == nil || txHash == "" {
		return nil
	}
	return p.sharedRepo.SettleResourceAllocation(ctx, txHash, receipt)
}

func (p *ResourceRentalService) ReleaseResource(ctx context.Context, allocationID id.ID, reason string) error {
	if p == nil || p.sharedRepo == nil || id.IsNil(allocationID) {
		return nil
	}
	return p.sharedRepo.ReleaseResourceAllocation(ctx, allocationID, reason)
}

func (p *ResourceRentalService) MaintainResources(ctx context.Context, workerID string) error {
	if p == nil || p.sharedRepo == nil {
		return nil
	}
	rentals, err := p.sharedRepo.ClaimResourceRentalsForMaintenance(ctx, workerID, p.maintenanceBatchLimit)
	if err != nil {
		return err
	}
	for _, rental := range rentals {
		claim := rental.MaintenanceClaim()
		if err := p.sharedRepo.RenewResourceRentalClaim(ctx, claim); err != nil {
			continue
		}
		provider, resolveErr := p.resolveProviderForRental(ctx, rental)
		if resolveErr != nil {
			_ = p.sharedRepo.MarkResourceRentalClaimHeld(ctx, claim, resolveErr.Error(), nil)
			continue
		}
		if rental.ExternalSerial == "" {
			_ = p.sharedRepo.MarkResourceRentalClaimHeld(ctx, claim, "resource rental has no provider serial", nil)
			continue
		}
		if rental.Status == "active" || rental.Status == "held" {
			if err := p.sharedRepo.MarkResourceRentalClaimReclaiming(ctx, claim); err != nil {
				_ = p.sharedRepo.ReleaseResourceRentalClaim(ctx, claim)
				continue
			}
			if _, err := provider.Reclaim(ctx, crypto.ResourceOrderRef{ExternalID: rental.ExternalSerial, ClientOrderID: rental.OutTradeNo}); err != nil {
				_ = p.sharedRepo.MarkResourceRentalClaimHeld(ctx, claim, err.Error(), nil)
				continue
			}
		}
		if err := p.sharedRepo.RenewResourceRentalClaim(ctx, claim); err != nil {
			continue
		}
		closed, err := p.recoverReclaimingRental(ctx, rental, provider)
		if err != nil {
			_ = p.sharedRepo.MarkResourceRentalClaimHeld(ctx, claim, err.Error(), nil)
			continue
		}
		if !closed {
			_ = p.sharedRepo.ReleaseResourceRentalClaim(ctx, claim)
		}
	}
	return nil
}

func (p *ResourceRentalService) recoverReclaimingRental(ctx context.Context, rental *ResourceRental, provider crypto.ExternalResourceProvider) (bool, error) {
	order, err := provider.Query(ctx, crypto.ResourceOrderRef{ExternalID: rental.ExternalSerial, ClientOrderID: rental.OutTradeNo})
	if err != nil {
		return false, err
	}
	if !order.Status.Terminal() {
		return false, nil
	}
	status := "closed"
	if order.Status == crypto.ResourceOrderFailed {
		status = "failed"
	}
	var closeErr error
	if !id.IsNil(rental.MaintenanceClaimToken) {
		closeErr = p.sharedRepo.CloseResourceRentalClaim(ctx, rental.MaintenanceClaim(), order.RefundAmount, order.ProviderBalance, order.ResponseJSON, status)
	} else {
		closeErr = p.sharedRepo.CloseResourceRental(ctx, rental.ID, order.RefundAmount, order.ProviderBalance, order.ResponseJSON, status)
	}
	if closeErr != nil {
		return false, closeErr
	}
	return true, nil
}

func resourceRequirementQuantity(req ResourceRequest, name string) int64 {
	for _, requirement := range req.Requirements {
		if requirement.Name == name {
			return requirement.Quantity
		}
	}
	return 0
}

func addEnergySafetyMargin(estimate, marginPct int64) int64 {
	if estimate <= 0 {
		return 0
	}
	if marginPct < 0 {
		marginPct = 0
	}
	return (estimate*(100+marginPct) + 99) / 100
}

func makeRentalOutTradeNo(tenantID string, rentalID id.ID) string {
	sum := sha256.Sum256([]byte(tenantID + "|" + rentalID.String()))
	return "mp_rr_" + hex.EncodeToString(sum[:])[:32]
}
