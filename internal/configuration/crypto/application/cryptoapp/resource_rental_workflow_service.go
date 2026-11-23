package cryptoapp

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

type ResourceRentalMetrics struct {
	ActiveRentals           int64           `json:"activeRentals"`
	ExpiringRentals         int64           `json:"expiringRentals"`
	ReservedEnergy          int64           `json:"reservedEnergy"`
	UnusedEnergy            int64           `json:"unusedEnergy"`
	AllocatedCost           types.RawAmount `json:"allocatedCost"`
	UnusedCost              types.RawAmount `json:"unusedCost"`
	ReclaimFailures         int64           `json:"reclaimFailures"`
	ProviderBalanceDelta    types.RawDelta  `json:"providerBalanceDelta"`
	SettlementRepairBacklog int64           `json:"settlementRepairBacklog"`
}

type ProviderBalanceIncident struct {
	ID                id.ID           `json:"id"`
	ProviderAccountID id.ID           `json:"providerAccountId"`
	TokenID           id.ID           `json:"tokenId"`
	Status            string          `json:"status"`
	LedgerAmount      types.RawDelta  `json:"ledgerAmount"`
	ProviderAmount    types.RawAmount `json:"providerAmount"`
	DeltaAmount       types.RawDelta  `json:"deltaAmount"`
	FirstSeenAt       time.Time       `json:"firstSeenAt"`
	LastSeenAt        time.Time       `json:"lastSeenAt"`
	OccurrenceCount   int             `json:"occurrenceCount"`
	ResolutionReason  string          `json:"resolutionReason"`
	ResolvedAt        *time.Time      `json:"resolvedAt"`
	ObservationID     *id.ID          `json:"observationId,omitempty"`
	ObservedAt        *time.Time      `json:"observedAt,omitempty"`
}

type ResourceAllocation struct {
	ID                  id.ID           `json:"id"`
	BasisType           string          `json:"basisType"`
	BasisID             id.ID           `json:"basisId"`
	Purpose             string          `json:"purpose"`
	OperationTokenID    id.ID           `json:"operationTokenId"`
	SourceWalletID      id.ID           `json:"sourceWalletId"`
	Status              string          `json:"status"`
	ReservedAmount      int64           `json:"reservedAmount"`
	ActualAmount        int64           `json:"actualAmount"`
	AllocatedCostAmount types.RawAmount `json:"allocatedCostAmount"`
	TxHash              string          `json:"txHash"`
	ChainResult         string          `json:"chainResult"`
	ReservedAt          time.Time       `json:"reservedAt"`
}

type ResourceRentalWorkflowRepository interface {
	GetResourceRentalMetrics(context.Context) (ResourceRentalMetrics, error)
	ListOpenProviderBalanceIncidents(context.Context) ([]ProviderBalanceIncident, error)
	AcknowledgeProviderBalanceIncident(context.Context, id.ID) (bool, error)
	ListResourceAllocations(context.Context, id.ID) ([]ResourceAllocation, error)
	QueueResourceRentalReclaim(context.Context, id.ID, id.ID) (bool, error)
	RetryHeldResourceRental(context.Context, id.ID, id.ID) (bool, error)
}

type ResourceRentalWorkflowService struct {
	repo ResourceRentalWorkflowRepository
}

func NewResourceRentalWorkflowService(repo ResourceRentalWorkflowRepository) *ResourceRentalWorkflowService {
	if repo == nil {
		panic("cryptoapp.NewResourceRentalWorkflowService: repository is required")
	}
	return &ResourceRentalWorkflowService{repo: repo}
}

func (s *ResourceRentalWorkflowService) Metrics(ctx context.Context) (ResourceRentalMetrics, error) {
	return s.repo.GetResourceRentalMetrics(ctx)
}

func (s *ResourceRentalWorkflowService) ListIncidents(ctx context.Context) ([]ProviderBalanceIncident, error) {
	return s.repo.ListOpenProviderBalanceIncidents(ctx)
}

func (s *ResourceRentalWorkflowService) ListAllocations(ctx context.Context, rentalID id.ID) ([]ResourceAllocation, error) {
	return s.repo.ListResourceAllocations(ctx, rentalID)
}

func (s *ResourceRentalWorkflowService) AcknowledgeIncident(ctx context.Context, incidentID id.ID) error {
	return s.mutate(ctx, func(txCtx context.Context) (bool, error) {
		return s.repo.AcknowledgeProviderBalanceIncident(txCtx, incidentID)
	}, "provider balance incident cannot be acknowledged")
}

func (s *ResourceRentalWorkflowService) Reclaim(ctx context.Context, rentalID, actorID id.ID) error {
	if id.IsNil(actorID) {
		return apperror.NewUnauthorized("authenticated user id is required")
	}
	return s.mutate(ctx, func(txCtx context.Context) (bool, error) {
		return s.repo.QueueResourceRentalReclaim(txCtx, rentalID, actorID)
	}, "only active rental can be reclaimed")
}

func (s *ResourceRentalWorkflowService) Retry(ctx context.Context, rentalID, actorID id.ID) error {
	if id.IsNil(actorID) {
		return apperror.NewUnauthorized("authenticated user id is required")
	}
	return s.mutate(ctx, func(txCtx context.Context) (bool, error) {
		return s.repo.RetryHeldResourceRental(txCtx, rentalID, actorID)
	}, "only held rental can be retried")
}

func (s *ResourceRentalWorkflowService) mutate(ctx context.Context, mutation func(context.Context) (bool, error), conflictMessage string) error {
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err)
	}
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		updated, err := mutation(txCtx)
		if err != nil {
			return err
		}
		if !updated {
			return apperror.NewValidation(conflictMessage)
		}
		return nil
	})
}
