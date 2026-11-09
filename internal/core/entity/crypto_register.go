package entity

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// ---------------------------------------------------------------------------
// Crypto Balance accumulation register
// ---------------------------------------------------------------------------

// CryptoBalanceMovement represents a movement in the crypto balance register.
// Tracks cryptocurrency amounts for wallets by token.
// Uses CryptoAmount (big.Int) instead of MinorUnits — ETH has 18 decimals.
type CryptoBalanceMovement struct {
	MovementBase

	// Dimensions
	WalletID id.ID `db:"wallet_id" json:"walletId"`
	TokenID  id.ID `db:"token_id" json:"tokenId"`

	// Resources — CryptoAmount for arbitrary precision
	Amount types.CryptoAmount `db:"amount" json:"amount"`
}

// NewCryptoBalanceMovement creates a new crypto balance movement.
func NewCryptoBalanceMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType RecordType,
	walletID, tokenID id.ID,
	amount types.CryptoAmount,
) CryptoBalanceMovement {
	return CryptoBalanceMovement{
		MovementBase: NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		WalletID:     walletID,
		TokenID:      tokenID,
		Amount:       amount,
	}
}

// ---------------------------------------------------------------------------
// Crypto Fee accumulation register
// ---------------------------------------------------------------------------

// FeeType defines the type of crypto processing fee.
type FeeType string

const (
	FeeTypeProcessing FeeType = "processing" // platform processing fee
	FeeTypeNetwork    FeeType = "network"    // blockchain network fee (gas)
	FeeTypeWithdrawal FeeType = "withdrawal" // withdrawal fee
	FeeTypeSweep      FeeType = "sweep"      // sweep consolidation fee
)

// CryptoFeeMovement represents a movement in the crypto fee register.
// Tracks platform fees, network fees, and withdrawal fees.
type CryptoFeeMovement struct {
	MovementBase

	// Dimensions
	MerchantID id.ID   `db:"merchant_id" json:"merchantId"`
	TokenID    id.ID   `db:"token_id" json:"tokenId"`
	FeeType    FeeType `db:"fee_type" json:"feeType"`

	// Resources — CryptoAmount for arbitrary precision
	Amount types.CryptoAmount `db:"amount" json:"amount"`
}

// NewCryptoFeeMovement creates a new crypto fee movement.
func NewCryptoFeeMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType RecordType,
	merchantID, tokenID id.ID,
	feeType FeeType,
	amount types.CryptoAmount,
) CryptoFeeMovement {
	return CryptoFeeMovement{
		MovementBase: NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		MerchantID:   merchantID,
		TokenID:      tokenID,
		FeeType:      feeType,
		Amount:       amount,
	}
}

// ---------------------------------------------------------------------------
// Crypto Platform Cost accumulation register
// ---------------------------------------------------------------------------

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
	MovementBase

	NetworkID        id.ID               `db:"network_id" json:"networkId"`
	OperationTokenID *id.ID              `db:"operation_token_id" json:"operationTokenId,omitempty"`
	CostTokenID      id.ID               `db:"cost_token_id" json:"costTokenId"`
	CostType         PlatformCostType    `db:"cost_type" json:"costType"`
	Purpose          PlatformCostPurpose `db:"purpose" json:"purpose"`
	Provider         string              `db:"provider" json:"provider"`
	ExternalOrderID  string              `db:"external_order_id" json:"externalOrderId"`
	Amount           types.CryptoAmount  `db:"amount" json:"amount"`
}

// NewCryptoPlatformCostMovement creates a new platform cost movement.
func NewCryptoPlatformCostMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType RecordType,
	networkID, operationTokenID, costTokenID id.ID,
	costType PlatformCostType,
	purpose PlatformCostPurpose,
	provider, externalOrderID string,
	amount types.CryptoAmount,
) CryptoPlatformCostMovement {
	operationTokenRef := operationTokenID
	return CryptoPlatformCostMovement{
		MovementBase:     NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
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
	recordType RecordType,
	networkID, costTokenID id.ID,
	costType PlatformCostType,
	purpose PlatformCostPurpose,
	provider, externalOrderID string,
	amount types.CryptoAmount,
) CryptoPlatformCostMovement {
	return CryptoPlatformCostMovement{
		MovementBase:    NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		NetworkID:       networkID,
		CostTokenID:     costTokenID,
		CostType:        costType,
		Purpose:         purpose,
		Provider:        provider,
		ExternalOrderID: externalOrderID,
		Amount:          amount,
	}
}

// CryptoProviderBalanceMovement tracks platform prepayments held by an
// external crypto infrastructure provider. Receipt increases the provider
// balance; expense decreases it.
type CryptoProviderBalanceMovement struct {
	MovementBase

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
	recordType RecordType,
	providerAccountID, tokenID id.ID,
	operationType, externalRef string,
	amount types.CryptoAmount,
) CryptoProviderBalanceMovement {
	return CryptoProviderBalanceMovement{
		MovementBase:      NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		ProviderAccountID: providerAccountID,
		TokenID:           tokenID,
		OperationType:     operationType,
		ExternalRef:       externalRef,
		Amount:            amount,
	}
}

// ---------------------------------------------------------------------------
// Crypto Merchant Balance accumulation register
// (Регистр «Крипто-расчёты с мерчантами»)
// Tracks platform debt to merchant: receipt = we owe more, expense = debt reduced.
// ---------------------------------------------------------------------------

// CryptoMerchantBalanceMovement represents a movement in the crypto merchant balance register.
// Receipt means the platform owes the merchant more (payment received).
// Expense means the platform paid the merchant (withdrawal processed).
type CryptoMerchantBalanceMovement struct {
	MovementBase

	// Dimensions
	MerchantID id.ID `db:"merchant_id" json:"merchantId"`
	TokenID    id.ID `db:"token_id" json:"tokenId"`

	// Resources — CryptoAmount for arbitrary precision
	Amount types.CryptoAmount `db:"amount" json:"amount"`
}

// NewCryptoMerchantBalanceMovement creates a new crypto merchant balance movement.
func NewCryptoMerchantBalanceMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType RecordType,
	merchantID, tokenID id.ID,
	amount types.CryptoAmount,
) CryptoMerchantBalanceMovement {
	return CryptoMerchantBalanceMovement{
		MovementBase: NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		MerchantID:   merchantID,
		TokenID:      tokenID,
		Amount:       amount,
	}
}
