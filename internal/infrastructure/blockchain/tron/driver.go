package tron

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
)

const (
	tronEnvelopeSchemaVersion   = 1
	defaultMaxFeeSun            = 100_000_000
	defaultNativeTransferFeeSun = 2_000_000
	defaultEnergyPriceSun       = 420
)

func DefaultProtocolMaxFee() types.RawAmount {
	return types.NewRawAmountFromInt64(defaultMaxFeeSun)
}

type DriverConfig struct {
	Network                crypto.NetworkRef
	RPC                    *RPCPool
	WatcherStateRepository crypto.WatcherStateRepository
	BlockCaptureRepository crypto.BlockCaptureRepository
	WalletDeriver          crypto.WalletDeriver
	MaxFeeMinor            types.RawAmount
	NativeTransferFeeMinor types.RawAmount
	EnergyPriceMinor       types.RawAmount
	QuoteTTL               time.Duration
}

type Factory struct {
	Configs map[id.ID]DriverConfig
}

func (f *Factory) Family() crypto.ChainFamily { return crypto.ChainFamilyTRON }

func (f *Factory) Capabilities() crypto.ChainFamilyCapabilities {
	return tronCapabilities()
}

func (f *Factory) Create(_ context.Context, network crypto.NetworkRef) (crypto.NetworkDriver, error) {
	if f == nil {
		return nil, errors.New("TRON driver factory is nil")
	}
	cfg, ok := f.Configs[network.ID]
	if !ok {
		return nil, fmt.Errorf("TRON runtime configuration missing for network_id %s", network.ID)
	}
	cfg.Network = network
	return NewDriver(cfg)
}

// Driver is the only adapter that translates family-neutral capabilities into
// TRON requests and receipts.
type Driver struct {
	network     crypto.NetworkRef
	rpc         *RPCPool
	stateRepo   crypto.WatcherStateRepository
	blockRepo   crypto.BlockCaptureRepository
	deriver     crypto.WalletDeriver
	maxFee      types.RawAmount
	nativeFee   types.RawAmount
	energyPrice types.RawAmount
	quoteTTL    time.Duration
}

func NewDriver(cfg DriverConfig) (*Driver, error) {
	if err := cfg.Network.Validate(); err != nil {
		return nil, fmt.Errorf("TRON driver network: %w", err)
	}
	if cfg.Network.Family != crypto.ChainFamilyTRON {
		return nil, fmt.Errorf("TRON driver cannot serve family %s", cfg.Network.Family)
	}
	if cfg.RPC == nil || cfg.WatcherStateRepository == nil || cfg.WalletDeriver == nil {
		return nil, errors.New("TRON driver requires RPC pool, watcher state repository, and Vault wallet deriver")
	}
	if !cfg.MaxFeeMinor.IsPositive() {
		return nil, errors.New("TRON driver fee configuration is invalid")
	}
	if !cfg.NativeTransferFeeMinor.IsPositive() {
		cfg.NativeTransferFeeMinor = types.NewRawAmountFromInt64(defaultNativeTransferFeeSun)
	}
	if !cfg.EnergyPriceMinor.IsPositive() {
		cfg.EnergyPriceMinor = types.NewRawAmountFromInt64(defaultEnergyPriceSun)
	}
	if cfg.QuoteTTL <= 0 {
		cfg.QuoteTTL = 30 * time.Second
	}
	return &Driver{
		network:     cfg.Network,
		rpc:         cfg.RPC,
		stateRepo:   cfg.WatcherStateRepository,
		blockRepo:   cfg.BlockCaptureRepository,
		deriver:     cfg.WalletDeriver,
		maxFee:      cfg.MaxFeeMinor,
		nativeFee:   cfg.NativeTransferFeeMinor,
		energyPrice: cfg.EnergyPriceMinor,
		quoteTTL:    cfg.QuoteTTL,
	}, nil
}

func (d *Driver) Network() crypto.NetworkRef                   { return d.network }
func (d *Driver) Capabilities() crypto.ChainFamilyCapabilities { return tronCapabilities() }
func (d *Driver) Watchers() crypto.WatcherFactory              { return nil }
func (d *Driver) Transactions() crypto.TransactionBuilder      { return d }
func (d *Driver) Broadcaster() crypto.Broadcaster              { return d }
func (d *Driver) Finality() crypto.FinalityReader              { return d }
func (d *Driver) Balances() crypto.BalanceReader               { return d }
func (d *Driver) Wallets() crypto.WalletDeriver                { return d }
func (d *Driver) Accounts() crypto.AccountLifecycle            { return d }
func (d *Driver) TokenAccounts() crypto.TokenAccountLifecycle  { return nil }
func (d *Driver) Addresses() crypto.AddressCodec               { return d }

func (d *Driver) RuntimeEndpoints() []crypto.RuntimeEndpointStatus {
	statuses := d.rpc.Status()
	result := make([]crypto.RuntimeEndpointStatus, 0, len(statuses))
	for _, status := range statuses {
		result = append(result, crypto.RuntimeEndpointStatus{
			EndpointID: status.EndpointID, AppliedRevision: status.AppliedRevision, Operations: status.Operations, LastError: status.LastError,
			ProviderID: status.ProviderID, Role: string(status.Role), State: string(status.State),
			Failures: status.Failures,
		})
	}
	return result
}

func (d *Driver) ProbeRuntime(ctx context.Context) { d.rpc.Probe(ctx) }

var _ crypto.DriverRuntimeReporter = (*Driver)(nil)
var _ crypto.DriverRuntimeProber = (*Driver)(nil)

func (d *Driver) QuoteFee(ctx context.Context, intent crypto.TransferIntent) (crypto.FeeQuote, error) {
	if err := d.validateIntent(intent); err != nil {
		return crypto.FeeQuote{}, err
	}
	estimated := d.nativeFee
	// Native TRON transactions do not carry a configurable fee_limit. Their
	// bounded configured estimate is therefore also the amount that must be
	// reserved and authorized. Reserving the unrelated smart-contract ceiling
	// for every transfer (and account activation) can deadlock a correctly
	// funded hot wallet while providing no additional on-chain protection.
	maxFee := estimated
	evidence := map[string]any{"model": "native_transfer"}
	var requirements []crypto.ResourceRequirement
	if intent.Asset.Standard == token.TokenStandardTRC20 {
		amount, err := intent.Amount.Int64Exact()
		if err != nil {
			return crypto.FeeQuote{}, fmt.Errorf("TRON transfer amount: %w", err)
		}
		estimate, err := d.rpc.EstimateEnergy(ctx, TriggerContractParams{
			OwnerAddress: intent.SourceAddress, ContractAddress: intent.Asset.ContractAddress,
			ToAddress: intent.DestinationAddress, Amount: big.NewInt(amount),
		})
		if err != nil {
			return crypto.FeeQuote{}, fmt.Errorf("TRON fee quote: %w", err)
		}
		estimated = d.energyPrice.MulDiv(estimate.EnergyRequired, 1)
		maxFee = estimated
		evidence = map[string]any{"model": "energy", "energyRequired": estimate.EnergyRequired, "energyPriceMinor": d.energyPrice.String()}
		requirements = []crypto.ResourceRequirement{{Name: "energy", Quantity: estimate.EnergyRequired}}
	}
	if estimated.Cmp(d.maxFee) > 0 {
		return crypto.FeeQuote{}, fmt.Errorf("TRON estimated fee %s exceeds configured max %s", estimated, d.maxFee)
	}
	raw, _ := json.Marshal(evidence)
	return crypto.FeeQuote{
		EstimatedFee:         estimated,
		MaxFee:               maxFee,
		NativeAsset:          crypto.AssetRef{TokenID: d.network.NativeTokenID, Standard: token.TokenStandardNative, Decimals: d.network.NativeDecimals},
		Payer:                crypto.FeePayerSourceWallet,
		ExpiresAt:            time.Now().UTC().Add(d.quoteTTL),
		RawEvidence:          raw,
		ResourceRequirements: requirements,
	}, nil
}

func (d *Driver) Build(ctx context.Context, intent crypto.TransferIntent, quote crypto.FeeQuote) (crypto.UnsignedTxEnvelope, error) {
	if err := d.validateIntent(intent); err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	if quote.ExpiresAt.IsZero() || !time.Now().Before(quote.ExpiresAt) || !quote.MaxFee.IsPositive() || quote.MaxFee.Cmp(d.maxFee) > 0 {
		return crypto.UnsignedTxEnvelope{}, errors.New("TRON fee quote is expired or outside policy")
	}
	var tx *UnsignedTransaction
	var err error
	amount, err := intent.Amount.Int64Exact()
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, fmt.Errorf("TRON transfer amount: %w", err)
	}
	switch intent.Asset.Standard {
	case token.TokenStandardNative:
		tx, err = d.rpc.CreateTransferTransaction(ctx, intent.SourceAddress, intent.DestinationAddress, amount)
	case token.TokenStandardTRC20:
		feeLimit, conversionErr := quote.MaxFee.Int64Exact()
		if conversionErr != nil {
			return crypto.UnsignedTxEnvelope{}, fmt.Errorf("TRON fee limit: %w", conversionErr)
		}
		tx, err = d.rpc.TriggerSmartContract(ctx, TriggerContractParams{
			OwnerAddress: intent.SourceAddress, ContractAddress: intent.Asset.ContractAddress,
			ToAddress: intent.DestinationAddress, Amount: big.NewInt(amount), FeeLimit: feeLimit,
		})
	default:
		return crypto.UnsignedTxEnvelope{}, fmt.Errorf("unsupported TRON asset standard %s", intent.Asset.Standard)
	}
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	payload, err := json.Marshal(tx)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, fmt.Errorf("marshal TRON unsigned envelope: %w", err)
	}
	fingerprint, err := crypto.CanonicalUnsignedFingerprint(crypto.ChainFamilyTRON, tronEnvelopeSchemaVersion, payload)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	return crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: tronEnvelopeSchemaVersion, UnsignedFingerprint: fingerprint, Generation: intent.EffectiveGeneration(), Payload: payload}, nil
}

func (d *Driver) Broadcast(ctx context.Context, envelope crypto.SignedTxEnvelope) (crypto.BroadcastOutcome, error) {
	return d.rpc.BroadcastSignedEnvelope(ctx, envelope)
}

func (d *Driver) NativeBalance(ctx context.Context, address string) (types.RawAmount, error) {
	if err := d.Validate(address); err != nil {
		return types.ZeroRawAmount(), err
	}
	return d.rpc.GetTRXBalance(ctx, address)
}

func (d *Driver) TokenBalance(ctx context.Context, asset crypto.AssetRef, address string) (types.RawAmount, error) {
	if asset.Standard != token.TokenStandardTRC20 || asset.ContractAddress == "" {
		return types.ZeroRawAmount(), errors.New("TRON token balance requires a TRC-20 contract")
	}
	if err := d.Validate(address); err != nil {
		return types.ZeroRawAmount(), err
	}
	return d.rpc.GetTRC20Balance(ctx, address, asset.ContractAddress)
}

func (d *Driver) DeriveAddress(ctx context.Context, network crypto.NetworkRef, derivationPath string) (crypto.WalletDerivation, error) {
	if err := d.validateNetwork(network); err != nil {
		return crypto.WalletDerivation{}, err
	}
	derived, err := d.deriver.DeriveAddress(ctx, network, derivationPath)
	if err != nil {
		return crypto.WalletDerivation{}, err
	}
	if err := d.Validate(derived.Address); err != nil {
		return crypto.WalletDerivation{}, fmt.Errorf("vault returned invalid TRON address: %w", err)
	}
	return derived, nil
}

func (d *Driver) IsActive(ctx context.Context, address string) (bool, error) {
	if err := d.Validate(address); err != nil {
		return false, err
	}
	return d.rpc.AccountExists(ctx, address)
}

func (d *Driver) BuildActivation(ctx context.Context, intent crypto.TransferIntent, quote crypto.FeeQuote) (crypto.UnsignedTxEnvelope, error) {
	if err := d.validateIntent(intent); err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	if quote.ExpiresAt.IsZero() || !time.Now().Before(quote.ExpiresAt) || quote.MaxFee.Cmp(d.maxFee) > 0 {
		return crypto.UnsignedTxEnvelope{}, errors.New("TRON activation fee quote is expired or outside policy")
	}
	tx, err := d.rpc.CreateAccountTransaction(ctx, intent.SourceAddress, intent.DestinationAddress)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	payload, err := json.Marshal(tx)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	fingerprint, err := crypto.CanonicalUnsignedFingerprint(crypto.ChainFamilyTRON, tronEnvelopeSchemaVersion, payload)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	return crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyTRON, SchemaVersion: tronEnvelopeSchemaVersion, UnsignedFingerprint: fingerprint, Generation: intent.EffectiveGeneration(), Payload: payload}, nil
}

func (d *Driver) Normalize(address string) (string, error) {
	normalized := NormalizeTronAddress(address)
	if err := ValidateTronAddress(normalized); err != nil {
		return "", err
	}
	return normalized, nil
}

func (d *Driver) Validate(address string) error { return ValidateTronAddress(address) }

func (d *Driver) validateNetwork(network crypto.NetworkRef) error {
	if network.ID != d.network.ID || network.Family != crypto.ChainFamilyTRON || network.NetworkIdentifier != d.network.NetworkIdentifier {
		return errors.New("TRON driver network identity mismatch")
	}
	return nil
}

func (d *Driver) validateIntent(intent crypto.TransferIntent) error {
	if err := d.validateNetwork(intent.Network); err != nil {
		return err
	}
	validAmount := intent.Amount.IsPositive() || (intent.Purpose == crypto.SigningPurposeActivation && intent.Amount.IsZero())
	if intent.BasisType == "" || intent.Attempt <= 0 || !validAmount {
		return errors.New("TRON transfer intent requires basis, positive attempt, and amount")
	}
	if err := d.Validate(intent.SourceAddress); err != nil {
		return fmt.Errorf("invalid TRON source: %w", err)
	}
	if err := d.Validate(intent.DestinationAddress); err != nil {
		return fmt.Errorf("invalid TRON destination: %w", err)
	}
	if intent.Asset.Standard == token.TokenStandardTRC20 && intent.Asset.ContractAddress == "" {
		return errors.New("TRC-20 intent requires contract address")
	}
	return nil
}

var _ crypto.NetworkDriver = (*Driver)(nil)
var _ crypto.WatcherLagDescriptor = (*Driver)(nil)
var _ crypto.CanonicalBlockReader = (*Driver)(nil)

func tronCapabilities() crypto.ChainFamilyCapabilities {
	return crypto.ChainFamilyCapabilities{
		Family:               crypto.ChainFamilyTRON,
		WalletProvisioning:   crypto.WalletProvisioningDerivedWithActivation,
		RequiredRPCRoles:     []string{"fullnode", "solidity"},
		ExternalResourceMode: crypto.ExternalResourceOptional,
		SequenceModel:        crypto.SequenceNone,
		CheckpointTimeUnit:   crypto.CheckpointUnixMillis,
		TokenAccountModel:    crypto.TokenAccountSameAddress,
		MinWatcherLagSeconds: 60,
	}
}

func (d *Driver) Close() { d.rpc.Close() }

func (d *Driver) Receipt(ctx context.Context, txHash string) (crypto.ChainReceipt, error) {
	if strings.TrimSpace(txHash) == "" {
		return crypto.ChainReceipt{}, errors.New("TRON receipt tx hash is required")
	}
	return d.quorumReceipt(ctx, txHash)
}
func (d *Driver) FinalizedBlock(ctx context.Context) (int64, string, error) {
	block, err := d.rpc.QuorumHead(ctx, chainconfig.RoleSolidity)
	if err != nil {
		return 0, "", err
	}
	return block.BlockHeader.RawData.Number, block.BlockID, nil
}
func (d *Driver) CanonicalBlock(ctx context.Context, height int64) (string, error) {
	if height < 0 {
		return "", errors.New("TRON canonical block height cannot be negative")
	}
	return d.rpc.AgreedBlockHash(ctx, height)
}
func (d *Driver) LagHints(string) crypto.WatcherLagHints {
	return crypto.WatcherLagHints{TimeUnit: crypto.CheckpointUnixMillis, TracksHeadHeight: true}
}
