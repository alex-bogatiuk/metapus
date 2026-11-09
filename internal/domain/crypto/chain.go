package crypto

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
)

// ChainFamily selects a chain implementation. It is deliberately broader than
// a concrete network: nile and mainnet are both served by the tron family.
type ChainFamily string

const (
	ChainFamilyTRON   ChainFamily = "tron"
	ChainFamilyEVM    ChainFamily = "evm"
	ChainFamilyTON    ChainFamily = "ton"
	ChainFamilySolana ChainFamily = "solana"
)

func (f ChainFamily) Valid() bool {
	switch f {
	case ChainFamilyTRON, ChainFamilyEVM, ChainFamilyTON, ChainFamilySolana:
		return true
	default:
		return false
	}
}

// NetworkRef is the immutable runtime profile resolved from network_id.
type NetworkRef struct {
	ID                    id.ID        `json:"id"`
	Code                  string       `json:"code"`
	Family                ChainFamily  `json:"family"`
	NetworkIdentifier     string       `json:"networkIdentifier"`
	NativeTokenID         id.ID        `json:"nativeTokenId"`
	NativeTokenSymbol     string       `json:"nativeTokenSymbol"`
	NativeDecimals        int          `json:"nativeDecimals"`
	RequiredConfirmations int          `json:"requiredConfirmations"`
	FinalityMode          FinalityMode `json:"finalityMode"`
}

func (n NetworkRef) Validate() error {
	if id.IsNil(n.ID) || id.IsNil(n.NativeTokenID) {
		return errors.New("network and native token IDs are required")
	}
	if n.Code == "" || n.NetworkIdentifier == "" || !n.Family.Valid() {
		return errors.New("network code, family, and identifier are required")
	}
	if n.NativeDecimals < 0 || n.NativeDecimals > 18 {
		return errors.New("native decimals must be between 0 and 18")
	}
	if n.RequiredConfirmations < 1 {
		return errors.New("required confirmations must be positive")
	}
	if !n.FinalityMode.Valid() {
		return errors.New("network finality mode is invalid")
	}
	return nil
}

// FinalityMode determines the network-specific financial boundary.
type FinalityMode string

const (
	FinalityByConfirmations       FinalityMode = "confirmations"
	FinalityByProtocolFinalized   FinalityMode = "protocol_finalized"
	FinalityByConfirmationsOrHead FinalityMode = "confirmations_or_finalized"
)

func (m FinalityMode) Valid() bool {
	return m == FinalityByConfirmations || m == FinalityByProtocolFinalized || m == FinalityByConfirmationsOrHead
}

type AssetRef struct {
	TokenID         id.ID               `json:"tokenId"`
	Code            string              `json:"code,omitempty"`
	Name            string              `json:"name,omitempty"`
	Symbol          string              `json:"symbol,omitempty"`
	Standard        token.TokenStandard `json:"standard"`
	ContractAddress string              `json:"contractAddress,omitempty"`
	Decimals        int                 `json:"decimals"`
}

type TransferIntent struct {
	BasisType          string             `json:"basisType"`
	Purpose            SigningPurpose     `json:"purpose"`
	BasisID            id.ID              `json:"basisId"`
	Attempt            int                `json:"attempt"`
	Generation         int                `json:"generation,omitzero"`
	Network            NetworkRef         `json:"network"`
	Asset              AssetRef           `json:"asset"`
	SourceWalletID     id.ID              `json:"sourceWalletId"`
	SourceAddress      string             `json:"sourceAddress"`
	DestinationAddress string             `json:"destinationAddress"`
	Amount             types.CryptoAmount `json:"amount"`
}

func (i TransferIntent) EffectiveGeneration() int {
	if i.Generation <= 0 {
		return 1
	}
	return i.Generation
}

type FeePayer string

const (
	FeePayerSourceWallet  FeePayer = "source_wallet"
	FeePayerNetworkWallet FeePayer = "network_fee_wallet"
)

type FeeQuote struct {
	EstimatedFee         types.CryptoAmount    `json:"estimatedFee"`
	MaxFee               types.CryptoAmount    `json:"maxFee"`
	NativeAsset          AssetRef              `json:"nativeAsset"`
	Payer                FeePayer              `json:"payer"`
	ExpiresAt            time.Time             `json:"expiresAt"`
	RawEvidence          json.RawMessage       `json:"rawEvidence,omitempty"`
	ResourceRequirements []ResourceRequirement `json:"resourceRequirements,omitempty"`
}

type ResourceRequirement struct {
	Name     string `json:"name"`
	Quantity int64  `json:"quantity"`
}

// WatcherState is the family-neutral durable checkpoint for one watcher
// stream. Family adapters may interpret LastBlock and LastTimestamp, but the
// persistence layer never imports a concrete chain package.
type WatcherState struct {
	NetworkID     id.ID     `db:"network_id" json:"networkId"`
	WatcherKey    string    `db:"watcher_key" json:"watcherKey"`
	LastBlock     int64     `db:"last_block" json:"lastBlock"`
	LastTimestamp int64     `db:"last_timestamp" json:"lastTimestamp"`
	Fingerprint   string    `db:"fingerprint" json:"fingerprint"`
	UpdatedAt     time.Time `db:"updated_at" json:"updatedAt"`
	LeaseOwner    string    `db:"lease_owner" json:"-"`
	FencingToken  int64     `db:"fencing_token" json:"-"`
}

type WatcherStateRepository interface {
	Get(ctx context.Context, networkID id.ID, watcherKey string) (*WatcherState, error)
	Save(ctx context.Context, state *WatcherState) error
}

var ErrWatcherStateNotFound = errors.New("watcher state not found")
var ErrWatcherLeaseHeld = errors.New("watcher lease held by another owner")
var ErrWatcherLeaseLost = errors.New("watcher lease lost")

// WatcherLeaseRepository fences checkpoint writes by a DB-issued generation.
// A caller must renew before scanning and release when stopping.
type WatcherLeaseRepository interface {
	Acquire(ctx context.Context, candidate *WatcherState, owner string) (*WatcherState, error)
	Renew(ctx context.Context, state *WatcherState) error
	Release(ctx context.Context, state *WatcherState) error
	Rewind(ctx context.Context, state *WatcherState, evidence string) error
}

type WatcherBlockFingerprint struct {
	BlockNumber int64
	BlockHash   string
	Timestamp   int64
}

// WatcherBlockHistoryRepository retains the bounded canonical history needed
// to locate a real common ancestor after a reorg.
type WatcherBlockHistoryRepository interface {
	GetBlockFingerprint(ctx context.Context, networkID id.ID, watcherKey string, blockNumber int64) (WatcherBlockFingerprint, bool, error)
	SaveBlockFingerprint(ctx context.Context, networkID id.ID, watcherKey string, block WatcherBlockFingerprint, retainFrom int64) error
	DeleteBlockFingerprintsAfter(ctx context.Context, networkID id.ID, watcherKey string, blockNumber int64) error
}

type OperationalHoldReason string

const (
	OperationalHoldDeepReorg       OperationalHoldReason = "deep_reorg"
	OperationalHoldPostedReorg     OperationalHoldReason = "posted_payment_reorg"
	OperationalHoldNonceDivergence OperationalHoldReason = "nonce_divergence"
)

type NetworkOperationalHoldRecorder interface {
	HoldNetwork(ctx context.Context, networkID id.ID, reason OperationalHoldReason, details map[string]any) error
}

// ResourceQuantity returns a normalized family resource requirement without
// exposing chain-specific RPC types to orchestration.
func (q FeeQuote) ResourceQuantity(name string) int64 {
	for _, requirement := range q.ResourceRequirements {
		if requirement.Name == name {
			return requirement.Quantity
		}
	}
	return 0
}

// UnsignedTxEnvelope is opaque to orchestration. Only the family driver and
// policy-aware signer may decode Payload.
type UnsignedTxEnvelope struct {
	Family              ChainFamily     `json:"family"`
	SchemaVersion       int             `json:"schemaVersion"`
	UnsignedFingerprint string          `json:"unsignedFingerprint"`
	Generation          int             `json:"generation"`
	NetworkSequence     string          `json:"networkSequence,omitempty"`
	Payload             json.RawMessage `json:"payload"`
}

type SignedTxEnvelope struct {
	Family              ChainFamily     `json:"family"`
	SchemaVersion       int             `json:"schemaVersion"`
	UnsignedFingerprint string          `json:"unsignedFingerprint"`
	Generation          int             `json:"generation"`
	NetworkSequence     string          `json:"networkSequence,omitempty"`
	TxHash              string          `json:"txHash"`
	Payload             json.RawMessage `json:"payload"`
}

type BroadcastStatus string

const (
	BroadcastAccepted     BroadcastStatus = "accepted"
	BroadcastAlreadyKnown BroadcastStatus = "already_known"
	BroadcastRejected     BroadcastStatus = "rejected"
	BroadcastAmbiguous    BroadcastStatus = "ambiguous"
)

type BroadcastOutcome struct {
	Status     BroadcastStatus `json:"status"`
	TxHash     string          `json:"txHash,omitempty"`
	ProviderID string          `json:"providerId,omitempty"`
	Reason     string          `json:"reason,omitempty"`
}

type ReceiptStatus string

const (
	ReceiptPending   ReceiptStatus = "pending"
	ReceiptSucceeded ReceiptStatus = "succeeded"
	ReceiptReverted  ReceiptStatus = "reverted"
)

type FinalityStatus string

const (
	FinalityProvisional FinalityStatus = "provisional"
	FinalitySolidified  FinalityStatus = "solidified"
	FinalityFinalized   FinalityStatus = "finalized"
)

type CanonicalityStatus string

const (
	CanonicalityUnknown   CanonicalityStatus = "unknown"
	CanonicalityCanonical CanonicalityStatus = "canonical"
	CanonicalityOrphaned  CanonicalityStatus = "orphaned"
)

type FeeComponent struct {
	Name        string             `json:"name"`
	Amount      types.CryptoAmount `json:"amount"`
	Quantity    int64              `json:"quantity,omitzero"`
	RawEvidence json.RawMessage    `json:"rawEvidence,omitempty"`
}

type FeeSettlementReport struct {
	NativeAsset AssetRef           `json:"nativeAsset"`
	TotalFee    types.CryptoAmount `json:"totalFee"`
	Components  []FeeComponent     `json:"components,omitempty"`
}

type ChainReceipt struct {
	FeeProof      *NetworkFeeProof    `json:"feeProof,omitempty"`
	NetworkID     id.ID               `json:"networkId"`
	TxHash        string              `json:"txHash"`
	Status        ReceiptStatus       `json:"status"`
	BlockHash     string              `json:"blockHash,omitempty"`
	BlockHeight   int64               `json:"blockHeight,omitzero"`
	Confirmations int                 `json:"confirmations"`
	Canonicality  CanonicalityStatus  `json:"canonicality"`
	Finality      FinalityStatus      `json:"finality"`
	Fee           FeeSettlementReport `json:"fee"`
	RawReceipt    json.RawMessage     `json:"rawReceipt,omitempty"`
	ObservedAt    time.Time           `json:"observedAt"`
}

// FinanciallyFinal reports whether a receipt may be posted to the ledger.
// Pending and provisional observations are never financially conclusive.
func (r ChainReceipt) FinanciallyFinal() bool {
	return r.Canonicality == CanonicalityCanonical &&
		(r.Finality == FinalitySolidified || r.Finality == FinalityFinalized) &&
		(r.Status == ReceiptSucceeded || r.Status == ReceiptReverted)
}

// CanonicalUnsignedFingerprint binds the family and schema version to the
// frozen schema-v1 encoding/json representation. Callers must compare it again
// after persistence and before signing/broadcasting. A future schema version
// must select its canonicalization explicitly instead of changing v1 bytes.
func CanonicalUnsignedFingerprint(family ChainFamily, schemaVersion int, payload json.RawMessage) (string, error) {
	if !family.Valid() || schemaVersion <= 0 {
		return "", errors.New("valid family and schema version are required")
	}
	if schemaVersion != 1 {
		return "", fmt.Errorf("unsupported unsigned fingerprint schema version: %d", schemaVersion)
	}
	canonical, err := json.CanonicalizeLegacyV1(payload)
	if err != nil {
		return "", fmt.Errorf("canonicalize unsigned payload: %w", err)
	}
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\n%d\n", family, schemaVersion)
	_, _ = h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Narrow chain capabilities. Orchestration depends on these contracts only.
// BlockchainEventSink is the acknowledgement boundary between a family
// watcher and financial processing. A watcher may advance its checkpoint only
// after Process returns nil.
type BlockchainEventSink interface {
	Process(ctx context.Context, event BlockchainEvent) error
}

type BlockchainEventSinkFunc func(ctx context.Context, event BlockchainEvent) error

func (f BlockchainEventSinkFunc) Process(ctx context.Context, event BlockchainEvent) error {
	if f == nil {
		return errors.New("blockchain event sink is nil")
	}
	return f(ctx, event)
}

type ManagedWatcher interface {
	Start(ctx context.Context, sink BlockchainEventSink) error
	ReplaceMonitoredAddresses(addresses []string)
}

type WatcherFactory interface {
	NewNativeWatcher(ctx context.Context, network NetworkRef, addresses []string) (ManagedWatcher, error)
	NewTokenWatcher(ctx context.Context, network NetworkRef, asset AssetRef, addresses []string) (ManagedWatcher, error)
}

type TransactionBuilder interface {
	QuoteFee(ctx context.Context, intent TransferIntent) (FeeQuote, error)
	Build(ctx context.Context, intent TransferIntent, quote FeeQuote) (UnsignedTxEnvelope, error)
}

// ReplacementTransactionBuilder is implemented by account-sequence chains
// that support fee-only replacement of a previously authorized transaction.
type ReplacementTransactionBuilder interface {
	BuildReplacement(ctx context.Context, intent TransferIntent, previous UnsignedTxEnvelope, policyMaxFee types.CryptoAmount) (UnsignedTxEnvelope, FeeQuote, error)
}

type ReplacementDisposition string

const (
	ReplacementUncertain     ReplacementDisposition = "uncertain"
	ReplacementStuck         ReplacementDisposition = "stuck"
	ReplacementMined         ReplacementDisposition = "mined"
	ReplacementNonceConsumed ReplacementDisposition = "nonce_consumed"
)

type ReplacementAssessment struct {
	Disposition  ReplacementDisposition
	MinedTxHash  string
	Observations int
	Reason       string
}

// ReplacementSafetyReader performs provider-diverse receipt and pending nonce
// checks before a fee replacement is authorized.
type ReplacementSafetyReader interface {
	AssessReplacement(ctx context.Context, sourceAddress, networkSequence string, txHashes []string) (ReplacementAssessment, error)
}

type NonceDivergenceHandler interface {
	HoldNonceDivergence(ctx context.Context, sourceAddress, reason string) error
}

// UnsignedSequenceReclaimer may release only an un-signed tail lease after a
// business operation fails before custody produces any signed bytes.
type UnsignedSequenceReclaimer interface {
	ReclaimUnsignedSequence(ctx context.Context, intent TransferIntent) error
}

type AssetRuntimeValidator interface {
	ValidateAsset(ctx context.Context, asset AssetRef) error
}

// NetworkRuntimePreflighter verifies family-specific protocol features before
// a configured network is allowed to start watchers or outbound execution.
type NetworkRuntimePreflighter interface {
	Preflight(ctx context.Context, assets []AssetRef) error
}

// NetworkStartupReconciler fences outbound execution until durable local
// sequence state has been compared with provider-diverse chain observations.
type NetworkStartupReconciler interface {
	ReconcileStartup(ctx context.Context) error
}

type Broadcaster interface {
	Broadcast(ctx context.Context, envelope SignedTxEnvelope) (BroadcastOutcome, error)
}

type FinalityReader interface {
	Receipt(ctx context.Context, txHash string) (ChainReceipt, error)
	FinalizedBlock(ctx context.Context) (height int64, hash string, err error)
}

// CanonicalBlockReader lets confirmation orchestration distinguish a
// genuinely orphaned previously-observed block from a temporarily unavailable
// transaction receipt. Provider errors must be returned as unknown evidence,
// never converted to orphaned.
type CanonicalBlockReader interface {
	CanonicalBlock(ctx context.Context, height int64) (hash string, err error)
}

type BalanceReader interface {
	NativeBalance(ctx context.Context, address string) (types.CryptoAmount, error)
	TokenBalance(ctx context.Context, asset AssetRef, address string) (types.CryptoAmount, error)
}

type WalletDerivation struct {
	Address        string `json:"address"`
	VaultKeyID     string `json:"vaultKeyId"`
	DerivationPath string `json:"derivationPath"`
}

type WalletDeriver interface {
	DeriveAddress(ctx context.Context, network NetworkRef, derivationPath string) (WalletDerivation, error)
}

type FamilyWalletDeriverResolver interface {
	ResolveDeriver(family ChainFamily) (WalletDeriver, error)
}

type FamilyWalletDeriverRegistry struct {
	mu       sync.RWMutex
	derivers map[ChainFamily]WalletDeriver
}

func NewFamilyWalletDeriverRegistry() *FamilyWalletDeriverRegistry {
	return &FamilyWalletDeriverRegistry{derivers: make(map[ChainFamily]WalletDeriver)}
}

func (r *FamilyWalletDeriverRegistry) Register(family ChainFamily, deriver WalletDeriver) error {
	if !family.Valid() || deriver == nil {
		return errors.New("wallet deriver with valid family is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.derivers[family]; exists {
		return fmt.Errorf("wallet deriver already registered: %s", family)
	}
	r.derivers[family] = deriver
	return nil
}

func (r *FamilyWalletDeriverRegistry) ResolveDeriver(family ChainFamily) (WalletDeriver, error) {
	r.mu.RLock()
	deriver := r.derivers[family]
	r.mu.RUnlock()
	if deriver == nil {
		return nil, fmt.Errorf("wallet deriver not found: %s", family)
	}
	return deriver, nil
}

type AccountLifecycle interface {
	IsActive(ctx context.Context, address string) (bool, error)
	BuildActivation(ctx context.Context, intent TransferIntent, quote FeeQuote) (UnsignedTxEnvelope, error)
}

// TokenAccountLifecycle materializes a per-asset receiving account (ATA, jetton
// wallet). Families with TokenAccountSameAddress return nil from TokenAccounts.
type TokenAccountLifecycle interface {
	IsReady(ctx context.Context, asset AssetRef, owner string) (bool, error)
	BuildMaterialization(ctx context.Context, intent TransferIntent, quote FeeQuote) (UnsignedTxEnvelope, error)
}

type AddressCodec interface {
	Normalize(address string) (string, error)
	Validate(address string) error
}

type NetworkDriver interface {
	Network() NetworkRef
	Capabilities() ChainFamilyCapabilities
	Watchers() WatcherFactory
	Transactions() TransactionBuilder
	Broadcaster() Broadcaster
	Finality() FinalityReader
	Balances() BalanceReader
	Wallets() WalletDeriver
	Accounts() AccountLifecycle // may return nil when activation is not a family concept
	TokenAccounts() TokenAccountLifecycle
	Addresses() AddressCodec
}

type DriverFactory interface {
	ChainFamilyCapabilityProvider
	Create(ctx context.Context, network NetworkRef) (NetworkDriver, error)
}

type ChainDriverRegistryBuilder interface {
	Build(ctx context.Context, networks []NetworkRef, derivers FamilyWalletDeriverResolver) (*ChainDriverRegistry, error)
}

var (
	ErrNetworkDriverNotFound  = errors.New("network driver not found")
	ErrDuplicateNetworkDriver = errors.New("network driver already registered")
)

// ChainDriverRegistry is keyed exclusively by catalog network ID. Codes are
// display/configuration keys and are never accepted for runtime routing.
type ChainDriverRegistry struct {
	mu      sync.RWMutex
	drivers map[id.ID]NetworkDriver
}

func NewChainDriverRegistry() *ChainDriverRegistry {
	return &ChainDriverRegistry{drivers: make(map[id.ID]NetworkDriver)}
}

func (r *ChainDriverRegistry) Register(driver NetworkDriver) error {
	if driver == nil {
		return errors.New("network driver is required")
	}
	network := driver.Network()
	if err := network.Validate(); err != nil {
		return fmt.Errorf("register network driver: %w", err)
	}
	capabilities := driver.Capabilities()
	if err := capabilities.Validate(); err != nil {
		return fmt.Errorf("register network driver capabilities: %w", err)
	}
	if capabilities.Family != network.Family {
		return fmt.Errorf("register network driver: capability family %s does not match network family %s", capabilities.Family, network.Family)
	}
	if capabilities.WalletProvisioning.Supported() && driver.Wallets() == nil {
		return errors.New("register network driver: wallet provisioning requires wallet derivation")
	}
	if capabilities.WalletProvisioning.RequiresActivation() && driver.Accounts() == nil {
		return errors.New("register network driver: provisioning mode requires account activation")
	}
	if capabilities.SupportsReplacement {
		if _, ok := driver.(ReplacementTransactionBuilder); !ok {
			return errors.New("register network driver: replacement support requires ReplacementTransactionBuilder")
		}
	}
	if capabilities.TokenAccountModel == TokenAccountDerivedPerAsset && driver.TokenAccounts() == nil {
		return errors.New("register network driver: derived token accounts require TokenAccountLifecycle")
	}
	if capabilities.TokenAccountModel == TokenAccountSameAddress && driver.TokenAccounts() != nil {
		return errors.New("register network driver: same-address token model cannot expose TokenAccountLifecycle")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.drivers[network.ID]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateNetworkDriver, network.ID)
	}
	r.drivers[network.ID] = driver
	return nil
}

func (r *ChainDriverRegistry) Resolve(networkID id.ID) (NetworkDriver, error) {
	if id.IsNil(networkID) {
		return nil, fmt.Errorf("%w: empty network ID", ErrNetworkDriverNotFound)
	}
	r.mu.RLock()
	driver := r.drivers[networkID]
	r.mu.RUnlock()
	if driver == nil {
		return nil, fmt.Errorf("%w: %s", ErrNetworkDriverNotFound, networkID)
	}
	return driver, nil
}

func (r *ChainDriverRegistry) Drivers() []NetworkDriver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	drivers := make([]NetworkDriver, 0, len(r.drivers))
	for _, driver := range r.drivers {
		drivers = append(drivers, driver)
	}
	return drivers
}

func BuildChainDriverRegistry(ctx context.Context, networks []NetworkRef, factories []DriverFactory) (*ChainDriverRegistry, error) {
	byFamily := make(map[ChainFamily]DriverFactory, len(factories))
	for _, factory := range factories {
		if factory == nil || !factory.Family().Valid() {
			return nil, errors.New("driver factory has invalid family")
		}
		capabilities := factory.Capabilities()
		if err := capabilities.Validate(); err != nil || capabilities.Family != factory.Family() {
			return nil, fmt.Errorf("driver factory has invalid capabilities for family %s", factory.Family())
		}
		if _, duplicate := byFamily[factory.Family()]; duplicate {
			return nil, fmt.Errorf("duplicate driver factory for family %s", factory.Family())
		}
		byFamily[factory.Family()] = factory
	}
	registry := NewChainDriverRegistry()
	var buildErrors []error
	for _, network := range networks {
		if err := network.Validate(); err != nil {
			buildErrors = append(buildErrors, fmt.Errorf("network %s: %w", network.Code, err))
			continue
		}
		factory := byFamily[network.Family]
		if factory == nil {
			buildErrors = append(buildErrors, fmt.Errorf("network %s: %w for family %s", network.Code, ErrNetworkDriverNotFound, network.Family))
			continue
		}
		driver, err := factory.Create(ctx, network)
		if err != nil {
			buildErrors = append(buildErrors, fmt.Errorf("create driver for network %s: %w", network.Code, err))
			continue
		}
		if driver == nil {
			buildErrors = append(buildErrors, fmt.Errorf("create driver for network %s: factory returned nil", network.Code))
			continue
		}
		if actual := driver.Network(); actual.ID != network.ID || actual.Family != network.Family || actual.NetworkIdentifier != network.NetworkIdentifier {
			buildErrors = append(buildErrors, fmt.Errorf("driver identity mismatch for network %s", network.Code))
			continue
		}
		if !driver.Capabilities().Equal(factory.Capabilities()) {
			buildErrors = append(buildErrors, fmt.Errorf("driver capability mismatch for network %s", network.Code))
			continue
		}
		if err := registry.Register(driver); err != nil {
			buildErrors = append(buildErrors, err)
		}
	}
	return registry, errors.Join(buildErrors...)
}

// WatcherBlockCommit is the durable boundary after every observation of a
// block has been acknowledged. Expected checkpoint plus fencing rejects stale
// consumers; history and checkpoint must commit in one transaction.
type WatcherBlockCommit struct {
	Candidate     WatcherState
	ExpectedBlock int64
	ExpectedHash  string
	Block         WatcherBlockFingerprint
	RetainFrom    int64
}
type WatcherBlockCommitter interface {
	CommitBlock(ctx context.Context, commit WatcherBlockCommit) error
}

// WatcherWindowCommit advances a timestamp-based stream only after the whole
// query window has been durably captured. Fencing and the expected checkpoint
// reject stale candidates; fingerprints are ephemeral provider page cursors.
type WatcherWindowCommit struct {
	Candidate         WatcherState
	ExpectedBlock     int64
	ExpectedTimestamp int64
}
type WatcherWindowCommitter interface {
	CommitWindow(ctx context.Context, commit WatcherWindowCommit) error
}

var ErrWatcherCheckpointConflict = errors.New("watcher checkpoint continuity conflict")

// IngestionHead is fresh RPC evidence, separate from database/API liveness.
type IngestionHead struct {
	Height     int64     `json:"height"`
	Hash       string    `json:"hash"`
	Timestamp  time.Time `json:"timestamp"`
	ObservedAt time.Time `json:"observedAt"`
}
type ChainIngestionReader interface {
	ReadIngestionHead(ctx context.Context, assets []AssetRef) (IngestionHead, error)
}

type IngestionCheckpoint struct {
	State          WatcherState
	LastProgressAt time.Time
	LeaseUntil     *time.Time
}

type IngestionOperationStatus struct {
	ProviderID string     `json:"providerId"`
	Operation  string     `json:"operation"`
	State      string     `json:"state"`
	Failures   int        `json:"failures"`
	LastError  string     `json:"lastError,omitempty"`
	LatencyMS  int64      `json:"latencyMs"`
	RetryAt    *time.Time `json:"retryAt,omitempty"`
}
type IngestionOperationReporter interface {
	IngestionOperations() []IngestionOperationStatus
}
