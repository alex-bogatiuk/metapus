package outboundapp

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
)

// ResourceProviderConnection binds an account snapshot to its concrete adapter.
// Credential decryption, rotation and client caching belong to Infrastructure.
type ResourceProviderConnection interface {
	Resolve(context.Context) (crypto.ExternalResourceProvider, error)
}

type ResourceProviderAccount struct {
	ID                    id.ID
	Provider              string
	NetworkID             id.ID
	BalanceTokenID        id.ID
	IsActive              bool
	IsFolder              bool
	CredentialsConfigured bool
	Connection            ResourceProviderConnection
}

type ResourceProviderCircuit interface {
	Allow(time.Time) bool
	Success()
	Failure(time.Time, int, time.Duration)
	CancelProbe()
}

type ResourceCircuitRegistry interface {
	Circuit(id.ID, string) ResourceProviderCircuit
}

type ResourceRental struct {
	MaintenanceClaimToken id.ID
	ID                    id.ID
	ProviderAccountID     id.ID
	NetworkID             id.ID
	ReceiveWalletID       id.ID
	CostTokenID           id.ID
	RequestedAmount       int64
	DelegatedAmount       int64
	GrossCostAmount       types.CryptoAmount
	RefundAmount          types.CryptoAmount
	Provider              string
	OutTradeNo            string
	ExternalSerial        string
	Status                string
	ExpiresAt             *time.Time
	ReceiveAddress        string
}

// ResourceRentalClaim identifies one attempt, including when a worker name is reused.
type ResourceRentalClaim struct {
	RentalID id.ID
	Token    id.ID
}

func (r *ResourceRental) MaintenanceClaim() ResourceRentalClaim {
	return ResourceRentalClaim{RentalID: r.ID, Token: r.MaintenanceClaimToken}
}

type ResourceAllocation struct {
	ID             id.ID
	RentalID       id.ID
	Generation     int
	Status         string
	ReservedAmount int64
}

type OrphanedResourceAllocation struct {
	AllocationID id.ID
	NetworkID    id.ID
	BasisType    string
	BasisID      id.ID
	TxHash       string
}

type ResourceRentalRepository interface {
	GetProviderAccount(ctx context.Context, accountID id.ID) (*ResourceProviderAccount, error)
	GetProviderAccountForRental(ctx context.Context, accountID id.ID) (*ResourceProviderAccount, error)
	GetActiveNetworkProviderAccounts(ctx context.Context, networkID id.ID) ([]*ResourceProviderAccount, error)
	GetResourceAllocationByBasis(ctx context.Context, basisType string, basisID id.ID) (*ResourceAllocation, error)
	TryReserveResource(ctx context.Context, req ResourceRequest, required int64, cutoff time.Duration) (*ResourceAllocation, *ResourceRental, error)
	CreateResourceRental(ctx context.Context, rental *ResourceRental, req ResourceRequest, requestJSON []byte) error
	ActivateResourceRental(ctx context.Context, rentalID id.ID, serial string, delegated int64, cost types.CryptoAmount, providerBalance *types.CryptoAmount, responseJSON []byte, expiresAt time.Time) error
	MarkResourceRentalHeld(ctx context.Context, rentalID id.ID, reason string, responseJSON []byte) error
	MarkResourceRentalReclaiming(ctx context.Context, rentalID id.ID) error
	CloseResourceRental(ctx context.Context, rentalID id.ID, refund types.CryptoAmount, providerBalance *types.CryptoAmount, responseJSON []byte, status string) error
	MarkResourceAllocationBroadcast(ctx context.Context, command ResourceBroadcastCommand) error
	SettleResourceAllocation(ctx context.Context, txHash string, receipt crypto.ChainReceipt) error
	ReleaseResourceAllocation(ctx context.Context, allocationID id.ID, reason string) error
	ClaimResourceRentalsForMaintenance(ctx context.Context, workerID string, limit int) ([]*ResourceRental, error)
	RenewResourceRentalClaim(ctx context.Context, claim ResourceRentalClaim) error
	MarkResourceRentalClaimReclaiming(ctx context.Context, claim ResourceRentalClaim) error
	MarkResourceRentalClaimHeld(ctx context.Context, claim ResourceRentalClaim, reason string, responseJSON []byte) error
	CloseResourceRentalClaim(ctx context.Context, claim ResourceRentalClaim, refund types.CryptoAmount, providerBalance *types.CryptoAmount, responseJSON []byte, status string) error
	ReleaseResourceRentalClaim(ctx context.Context, claim ResourceRentalClaim) error
}
