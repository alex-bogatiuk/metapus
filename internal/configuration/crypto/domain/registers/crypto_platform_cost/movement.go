package crypto_platform_cost

import (
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// PlatformCostType defines platform-borne crypto processing cost categories.
type PlatformCostType string

const (
	PlatformCostTypeChainResourceRent PlatformCostType = "chain_resource_rent"
	PlatformCostTypeChainNetworkFee   PlatformCostType = "chain_network_fee"
	PlatformCostTypeCustodyLoss       PlatformCostType = "custody_loss"
)

// PlatformCostPurpose defines the business workflow that caused the cost.
type PlatformCostPurpose string

const (
	PlatformCostPurposeSweep          PlatformCostPurpose = "sweep"
	PlatformCostPurposeWithdrawal     PlatformCostPurpose = "withdrawal"
	PlatformCostPurposeTreasury       PlatformCostPurpose = "treasury"
	PlatformCostPurposeActivation     PlatformCostPurpose = "activation"
	PlatformCostPurposeResourceRental PlatformCostPurpose = "resource_rental"
	PlatformCostPurposeCustodyLoss    PlatformCostPurpose = "custody_loss"
)

// CryptoPlatformCostMovement records platform-owned crypto operating costs.
// It never changes merchant balances or platform fee revenue balances.
type CryptoPlatformCostMovement struct {
	entity.MovementBase

	NetworkID        id.ID               `db:"network_id" json:"networkId"`
	OperationTokenID *id.ID              `db:"operation_token_id" json:"operationTokenId,omitempty"`
	CostTokenID      id.ID               `db:"cost_token_id" json:"costTokenId"`
	CostType         PlatformCostType    `db:"cost_type" json:"costType"`
	Purpose          PlatformCostPurpose `db:"purpose" json:"purpose"`
	Provider         string              `db:"provider" json:"provider"`
	ExternalOrderID  string              `db:"external_order_id" json:"externalOrderId"`
	Amount           types.RawAmount     `db:"amount" json:"amount"`
}

// NewCryptoPlatformCostMovement creates a new platform cost movement.
func NewCryptoPlatformCostMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType entity.RecordType,
	networkID, operationTokenID, costTokenID id.ID,
	costType PlatformCostType,
	purpose PlatformCostPurpose,
	provider, externalOrderID string,
	amount types.RawAmount,
) CryptoPlatformCostMovement {
	operationTokenRef := operationTokenID
	return CryptoPlatformCostMovement{
		MovementBase:     entity.NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		NetworkID:        networkID,
		OperationTokenID: &operationTokenRef,
		CostTokenID:      costTokenID,
		CostType:         costType,
		Purpose:          purpose,
		Provider:         provider,
		ExternalOrderID:  externalOrderID,
		Amount:           amount,
	}
}

// NewSharedCryptoPlatformCostMovement creates a platform cost that is not
// attributable to one operation token, such as a reusable resource rental.
func NewSharedCryptoPlatformCostMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType entity.RecordType,
	networkID, costTokenID id.ID,
	costType PlatformCostType,
	purpose PlatformCostPurpose,
	provider, externalOrderID string,
	amount types.RawAmount,
) CryptoPlatformCostMovement {
	return CryptoPlatformCostMovement{
		MovementBase:    entity.NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		NetworkID:       networkID,
		CostTokenID:     costTokenID,
		CostType:        costType,
		Purpose:         purpose,
		Provider:        provider,
		ExternalOrderID: externalOrderID,
		Amount:          amount,
	}
}
