package outboundapp

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
)

const (
	SettlementWithdrawal = "withdrawal"
	SettlementSweep      = "sweep"
	SettlementTreasury   = "treasury"
)

type SettlementTask struct {
	Kind             string
	DocumentID       id.ID
	LineID           id.ID
	RequestID        id.ID
	MerchantID       id.ID
	NetworkID        id.ID
	OperationTokenID id.ID
	SourceWalletID   id.ID
	HotWalletID      id.ID
	TxHash           string
	Amount           types.CryptoAmount
	RequiredConfs    int
	PostedVersion    int
	VerificationID   *id.ID
}

type ReplacementCandidate struct {
	AuthorizationID string
	BasisType       string
	BasisID         id.ID
	Purpose         crypto.SigningPurpose
	Attempt         int
	Generation      int
}

type SigningGeneration struct {
	AuthorizationID     string
	Generation          int
	Status              string
	TxHash              string
	UnsignedFingerprint string
	Signed              *crypto.SignedTxEnvelope
}

type SettlementRepository interface {
	HoldOutboundSettlementClaim(context.Context, SettlementTask, error) error
	ReleaseOutboundSettlementClaim(context.Context, SettlementTask) error
	RecordOutboundSettlement(context.Context, SettlementTask, crypto.ChainReceipt) error
	ListOrphanedResourceAllocations(context.Context, int) ([]OrphanedResourceAllocation, error)
	// Financial admission locks the document's resources and reports whether
	// the principal still awaits recognition (false: only the fee settles).
	PrepareSweepSettlement(context.Context, SettlementTask, crypto.ChainReceipt) (bool, error)
	PrepareTreasurySettlement(context.Context, SettlementTask, crypto.ChainReceipt) (bool, error)
	PrepareWithdrawalSettlement(context.Context, SettlementTask, crypto.ChainReceipt) (bool, error)
}

type SweepSettlementEvidence interface {
	VerifySweep(context.Context, crypto.NetworkDriver, SettlementTask, crypto.ChainReceipt) (id.ID, error)
}

type TreasurySettlementEvidence interface {
	VerifyTreasury(context.Context, crypto.NetworkDriver, SettlementTask, crypto.ChainReceipt) (id.ID, error)
}

type WithdrawalSettlementEvidence interface {
	VerifyWithdrawal(context.Context, crypto.NetworkDriver, SettlementTask, crypto.ChainReceipt) (id.ID, error)
}

type SettlementFees interface {
	Get(context.Context, string, id.ID, crypto.SigningPurpose, int) (crypto.FeeReservation, bool, error)
	Settle(context.Context, id.ID, crypto.ChainReceipt) error
}

type SettlementObserver interface {
	Start(context.Context, string, id.ID, crypto.SigningPurpose) (context.Context, func(error))
	Finality(context.Context, crypto.NetworkRef) (context.Context, func(error))
}

type ReplacementRepository interface {
	GetSigningAttempt(context.Context, string, id.ID, crypto.SigningPurpose, int, int) (SigningAuthorizationRecord, bool, error)
	ListSigningGenerations(context.Context, ReplacementCandidate) ([]SigningGeneration, error)
	LoadSigningAsset(context.Context, crypto.SigningAuthorization) (crypto.AssetRef, error)
	UpdateOutboundReplacementHash(context.Context, string, id.ID, string, string) error
	MarkSigningAuthorizationHeld(context.Context, string, string) error
	MarkSigningAuthorizationFailed(context.Context, string, string) error
	MarkSigningAuthorizationBroadcast(context.Context, string, string) error
	MarkSigningAuthorizationSuperseded(context.Context, string, string) error
	MarkSigningAuthorizationWinner(context.Context, string, string) error
}

type ReplacementFees interface {
	FeeBudget
	IncreaseMaxFee(context.Context, id.ID, types.CryptoAmount, int) error
	MarkReplacementBroadcast(context.Context, id.ID, string, string, string, string, int) error
}

type ReplacementObserver interface {
	Broadcast(context.Context, crypto.NetworkRef, crypto.SigningPurpose, crypto.BroadcastOutcome)
	Replacement(context.Context, id.ID, int, string)
}

type NativeFundingConfirmationRepository interface {
	ClaimNativeFundingConfirmations(context.Context, string, int) ([]crypto.NativeFunding, error)
	ReleaseNativeFundingConfirmation(context.Context, id.ID, string) error
}

type FundingConfirmer interface {
	Confirm(context.Context, crypto.NetworkDriver, crypto.NativeFunding) (bool, error)
}

const FundingConfirmationTimeout = 30 * time.Second
