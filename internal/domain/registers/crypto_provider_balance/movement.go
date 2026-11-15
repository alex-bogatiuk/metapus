package crypto_provider_balance

import (
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// CryptoProviderBalanceMovement tracks platform prepayments held by an
// external crypto infrastructure provider. Receipt increases the provider
// balance; expense decreases it.
type CryptoProviderBalanceMovement struct {
	entity.MovementBase

	ProviderAccountID id.ID              `db:"provider_account_id" json:"providerAccountId"`
	TokenID           id.ID              `db:"token_id" json:"tokenId"`
	OperationType     string             `db:"operation_type" json:"operationType"`
	ExternalRef       string             `db:"external_ref" json:"externalRef"`
	Amount            types.CryptoAmount `db:"amount" json:"amount"`
}

// NewCryptoProviderBalanceMovement creates one immutable provider prepayment movement.
func NewCryptoProviderBalanceMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType entity.RecordType,
	providerAccountID, tokenID id.ID,
	operationType, externalRef string,
	amount types.CryptoAmount,
) CryptoProviderBalanceMovement {
	return CryptoProviderBalanceMovement{
		MovementBase:      entity.NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		ProviderAccountID: providerAccountID,
		TokenID:           tokenID,
		OperationType:     operationType,
		ExternalRef:       externalRef,
		Amount:            amount,
	}
}
