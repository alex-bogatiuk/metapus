package outboundapp

import (
	"context"
	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_sweep"
	"metapus/internal/configuration/crypto/domain/documents/crypto_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/treasury_withdrawal"
	"metapus/internal/configuration/crypto/domain/registers/crypto_platform_cost"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
	"time"
)

type SigningAuthorizationRecord struct {
	RenewalID      *id.ID
	ID             id.ID
	Authorization  crypto.SigningAuthorization
	JWS            string
	ApprovalBundle crypto.ApprovalBundle
	UnsignedTx     crypto.UnsignedTxEnvelope
	SignedTx       *crypto.SignedTxEnvelope
	TxID           string
	Status         string
	SourceWallet   HotWallet
}
type TreasuryTask struct {
	RecoveryOnly    bool
	WithdrawalID    id.ID
	TokenID         id.ID
	SourceWalletID  id.ID
	NetworkID       id.ID
	NetworkCode     string
	Amount          types.RawAmount
	DestAddress     string
	ContractAddress string
	TokenStandard   token.TokenStandard
	TokenDecimals   int
	RequiredConfs   int
	PostedVersion   int
}
type SweepTask struct {
	RecoveryOnly          bool
	AttemptID             id.ID
	SweepID               *id.ID
	WalletID              id.ID
	TokenID               id.ID
	NetworkID             id.ID
	NetworkCode           string
	Address               string
	VaultKeyID            string
	ContractAddress       string
	TokenStandard         token.TokenStandard
	TokenDecimals         int
	ChainActivationStatus wallet.ChainActivationStatus
	Balance               types.RawAmount
	RequiredConfs         int
	HotWalletID           id.ID
	HotWalletAddress      string
	HotWalletVaultKeyID   string
	HotWalletBalance      types.RawAmount
}
type FeeReservationRequest = crypto.FeeReservationRequest

type SigningAttemptInput struct {
	Intent          crypto.TransferIntent
	Capabilities    crypto.ChainFamilyCapabilities
	MerchantID      *id.ID
	VaultKeyID      string
	FeePayerAddress string
	MaxNetworkFee   types.RawAmount
	Unsigned        crypto.UnsignedTxEnvelope
	ApprovalBundle  crypto.ApprovalBundle
}
type ResourceRequest struct {
	TenantID                 string
	BasisType                string
	BasisID                  id.ID
	RentalBasisType          string
	RentalBasisID            id.ID
	Purpose                  crypto_platform_cost.PlatformCostPurpose
	NetworkID                id.ID
	OperationTokenID         id.ID
	SourceWalletID           id.ID
	SourceAddress            string
	ContractAddress          string
	DestinationAddress       string
	Amount                   types.RawAmount
	Requirements             []crypto.ResourceRequirement
	ProviderAccountIDs       []id.ID
	ProviderFailureThreshold int
	ProviderOpenDuration     time.Duration
}
type ResourceReservationResult struct {
	AllocationID  id.ID
	RentalID      id.ID
	ReservedUnits int64
	ResourceUsed  bool
}
type ResourcePreparer interface {
	PrepareResource(ctx context.Context, req ResourceRequest) (ResourceReservationResult, error)
	MarkResourceBroadcast(ctx context.Context, command ResourceBroadcastCommand) error
	SettleResource(ctx context.Context, txHash string, receipt crypto.ChainReceipt) error
	ReleaseResource(ctx context.Context, allocationID id.ID, reason string) error
	MaintainResources(ctx context.Context, workerID string) error
}
type ResourceBroadcastCommand struct {
	AllocationID id.ID
	BasisType    string
	BasisID      id.ID
	TxHash       string
}
type ResourcePreparerResolver interface {
	Resolve(family crypto.ChainFamily) ResourcePreparer
}
type SweepPlatformCostLine struct {
	OrderID         id.ID
	NetworkID       id.ID
	CostTokenID     id.ID
	CostType        crypto_platform_cost.PlatformCostType
	Provider        string
	ExternalOrderID string
	Amount          types.RawAmount
}
type NativeFundingRequest struct {
	BasisType string
	BasisID   id.ID
	NetworkID id.ID
	WalletID  id.ID
	Address   string
}

func (t SweepTask) HotWallet() HotWallet {
	return HotWallet{
		ID:         t.HotWalletID,
		Address:    t.HotWalletAddress,
		VaultKeyID: t.HotWalletVaultKeyID,
		Balance:    t.HotWalletBalance,
	}
}

type HotWallet = crypto.SpendWallet

// TreasuryRepository preserves the locking and immutable-attempt operations of persistence.
type TreasuryRepository interface {
	GetTreasurySigningAttempt(context.Context, id.ID) (HotWallet, SigningAuthorizationRecord, bool, error)
	ValidateExternalWithdrawalDestination(context.Context, id.ID, string) error
	ReserveFeeForTreasury(context.Context, id.ID, id.ID, types.RawAmount) error
	ReserveHotWalletForTreasury(context.Context, TreasuryTask) (HotWallet, error)
	LoadTreasuryFeeReservationLines(context.Context, id.ID) ([]treasury_withdrawal.FeeLine, error)
	ConsumeTreasuryReservations(context.Context, id.ID) error
	MarkSigningAuthorizationBroadcast(context.Context, string, string) error
	MarkSigningAuthorizationFailed(context.Context, string, string) error
	MarkSigningAuthorizationHeld(context.Context, string, string) error
	MarkTreasuryHeld(context.Context, id.ID, string) error
}
type FeeBudget interface {
	Get(context.Context, string, id.ID, crypto.SigningPurpose, int) (crypto.FeeReservation, bool, error)
	LoadPolicy(context.Context, id.ID) (crypto.NetworkPolicy, error)
	Reserve(context.Context, FeeReservationRequest) (crypto.FeeReservation, error)
	BindUnsignedFingerprint(context.Context, id.ID, string) error
	Hold(context.Context, id.ID, string) error
	Release(context.Context, id.ID, string) error
	MarkBroadcast(context.Context, id.ID, string, string) error
}
type Authorizations interface {
	PrepareTransferCustody(context.Context, crypto.TransferIntent, *id.ID) (*id.ID, error)
	ResolveSigner(context.Context, *crypto.FamilySignerRegistry, crypto.NetworkRef) (crypto.PolicyAwareTransactionSigner, error)
	Create(context.Context, SigningAttemptInput) (SigningAuthorizationRecord, error)
	SignOrReuse(context.Context, crypto.PolicyAwareTransactionSigner, SigningAuthorizationRecord) (crypto.SignedTxEnvelope, error)
	CheckCustodyDispatch(context.Context, crypto.SigningAuthorization, *crypto.FamilySignerRegistry) error
}
type ApprovalBundles interface {
	LoadBundle(context.Context, string, id.ID) (crypto.ApprovalBundle, bool, error)
}

// Observer owns telemetry; it has no authority over financial state.
type Observer interface {
	Start(context.Context, string, id.ID, crypto.SigningPurpose) (context.Context, func(error))
	Broadcast(context.Context, crypto.NetworkRef, crypto.SigningPurpose, crypto.BroadcastOutcome)
	ResourcePreparation(context.Context, crypto.NetworkPolicy) (context.Context, func(crypto.FeeStrategy, error))
}

type SweepRepository interface {
	cryptoapp.SweepLifecycleRepository
	CancelSweepAttemptAllocation(ctx context.Context, sweepAttemptID id.ID, reason, actor string) error
	EnsureSweepExecutionDocument(
		ctx context.Context,
		attemptIDs []id.ID,
		candidate *crypto_sweep.CryptoSweep,
		repo crypto_sweep.Repository,
	) (*crypto_sweep.CryptoSweep, error)
	GetSweepSigningAttempt(
		ctx context.Context,
		task SweepTask,
	) (SigningAuthorizationRecord, bool, error)
	MarkSigningAuthorizationBroadcast(ctx context.Context, authID, txID string) error
	MarkSigningAuthorizationFailed(ctx context.Context, authID, txID string) error
	MarkSigningAuthorizationHeld(ctx context.Context, authID, txID string) error
	MarkSweepAttemptBroadcast(ctx context.Context, attemptID id.ID) error
	MarkSweepAttemptFailed(ctx context.Context, attemptID id.ID, reason string) error
	MarkSweepAttemptHeld(ctx context.Context, attemptID id.ID, reason string) error
	MarkSweepAttemptResourceReady(ctx context.Context, attemptID id.ID) error
	MarkSweepAttemptSigning(ctx context.Context, attemptID id.ID) error
	MarkSweepExecutionStatus(ctx context.Context, sweepID id.ID, status string) error
	RecordSweepLineBroadcast(ctx context.Context, sweepID, lineID id.ID, txHash string, amount types.RawAmount) error
	ReleaseSweepClaims(ctx context.Context, walletIDs []id.ID) error
}
type NativeFunding interface {
	Ensure(context.Context, crypto.NetworkDriver, crypto.NetworkPolicy, NativeFundingRequest, crypto.PolicyAwareTransactionSigner, types.RawAmount) (bool, error)
}

type WithdrawalTask = crypto.WithdrawalExecutionTask
type WithdrawalCompletionState struct {
	DocumentID *id.ID
	Status     string
}
type WithdrawalRepository interface {
	ConsumeWithdrawalReservation(ctx context.Context, requestID id.ID) error
	EnsureAMLWithdrawalExecution(ctx context.Context, task WithdrawalTask, candidate *crypto_withdrawal.CryptoWithdrawal, repo crypto_withdrawal.Repository) (*crypto_withdrawal.CryptoWithdrawal, error)
	EnsureWithdrawalExecutionDocument(
		ctx context.Context,
		requestID id.ID,
		candidate *crypto_withdrawal.CryptoWithdrawal,
		repo crypto_withdrawal.Repository,
	) (*crypto_withdrawal.CryptoWithdrawal, error)
	GetSigningAttempt(ctx context.Context, basisType string, basisID id.ID, purpose crypto.SigningPurpose, attempt, generation int) (SigningAuthorizationRecord, bool, error)
	GetWithdrawalSigningAttempt(
		ctx context.Context,
		requestID id.ID,
	) (HotWallet, SigningAuthorizationRecord, bool, error)
	LinkAMLWithdrawalBroadcast(ctx context.Context, task WithdrawalTask, authorizationID, txHash string) error
	LinkWithdrawalBroadcast(ctx context.Context, requestID, withdrawalID id.ID) error
	LockWithdrawalCompletion(ctx context.Context, task WithdrawalTask) (WithdrawalCompletionState, error)
	MarkSigningAuthorizationBroadcast(ctx context.Context, authID, txID string) error
	MarkSigningAuthorizationHeld(ctx context.Context, authID, txID string) error
	MarkWithdrawalExecutionSigned(ctx context.Context, withdrawalID id.ID) error
	MarkWithdrawalHeld(ctx context.Context, requestID id.ID, reason string) error
	ReserveHotWalletForWithdrawal(
		ctx context.Context,
		requestID, merchantID, networkID, tokenID id.ID,
		required types.RawAmount,
	) (HotWallet, error)
	ValidateExternalWithdrawalDestination(
		ctx context.Context,
		networkID id.ID,
		destination string,
	) error
}
