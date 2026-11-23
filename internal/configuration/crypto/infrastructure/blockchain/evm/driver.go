package evm

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/token"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/logger"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

const (
	evmEnvelopeSchemaVersion = 1
	defaultQuoteTTL          = 30 * time.Second
	defaultMaxFeeWei         = "1000000000000000000"
)

func DefaultProtocolMaxFee() types.RawAmount {
	return types.MustRawAmount(defaultMaxFeeWei)
}

var erc20ABI = mustABI(`[
  {"type":"function","name":"transfer","stateMutability":"nonpayable","inputs":[{"name":"to","type":"address"},{"name":"value","type":"uint256"}],"outputs":[{"name":"","type":"bool"}]},
  {"type":"function","name":"balanceOf","stateMutability":"view","inputs":[{"name":"account","type":"address"}],"outputs":[{"name":"","type":"uint256"}]},
  {"type":"function","name":"decimals","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint8"}]},
  {"type":"function","name":"symbol","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"string"}]},
  {"type":"function","name":"name","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"string"}]},
  {"type":"event","name":"Transfer","anonymous":false,"inputs":[{"name":"from","type":"address","indexed":true},{"name":"to","type":"address","indexed":true},{"name":"value","type":"uint256","indexed":false}]}
]`)

func mustABI(raw string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	return parsed
}

type DriverConfig struct {
	Network                crypto.NetworkRef
	RPC                    *RPCPool
	WatcherStateRepository crypto.WatcherStateRepository
	WatcherBlockHistory    crypto.WatcherBlockHistoryRepository
	NetworkHoldRecorder    crypto.NetworkOperationalHoldRecorder
	WalletDeriver          crypto.WalletDeriver
	NonceManager           NonceManager
	MaxFeeMinor            types.RawAmount
	QuoteTTL               time.Duration
	WatcherPollInterval    time.Duration
	WatcherLookback        int64
	WatcherSettings        chainconfig.EVMSettings
}

type Factory struct{ Configs map[id.ID]DriverConfig }

func (f *Factory) Family() crypto.ChainFamily                   { return crypto.ChainFamilyEVM }
func (f *Factory) Capabilities() crypto.ChainFamilyCapabilities { return evmCapabilities() }
func (f *Factory) Create(_ context.Context, network crypto.NetworkRef) (crypto.NetworkDriver, error) {
	if f == nil {
		return nil, errors.New("EVM driver factory is nil")
	}
	cfg, ok := f.Configs[network.ID]
	if !ok {
		return nil, fmt.Errorf("EVM runtime configuration missing for network_id %s", network.ID)
	}
	cfg.Network = network
	return NewDriver(cfg)
}

type Driver struct {
	network         crypto.NetworkRef
	rpc             *RPCPool
	stateRepo       crypto.WatcherStateRepository
	historyRepo     crypto.WatcherBlockHistoryRepository
	holdRecorder    crypto.NetworkOperationalHoldRecorder
	deriver         crypto.WalletDeriver
	nonces          NonceManager
	maxFee          types.RawAmount
	quoteTTL        time.Duration
	pollInterval    time.Duration
	lookback        int64
	watcherSettings chainconfig.EVMSettings
}

func NewDriver(cfg DriverConfig) (*Driver, error) {
	if err := cfg.Network.Validate(); err != nil {
		return nil, fmt.Errorf("EVM driver network: %w", err)
	}
	if cfg.Network.Family != crypto.ChainFamilyEVM {
		return nil, fmt.Errorf("EVM driver cannot serve family %s", cfg.Network.Family)
	}
	if cfg.RPC == nil || cfg.WatcherStateRepository == nil || cfg.WatcherBlockHistory == nil || cfg.NetworkHoldRecorder == nil || cfg.WalletDeriver == nil || cfg.NonceManager == nil {
		return nil, errors.New("EVM driver requires RPC pool, watcher state/history, hold recorder, Vault wallet deriver, and nonce manager")
	}
	if _, ok := cfg.WatcherStateRepository.(crypto.WatcherBlockCommitter); !ok {
		return nil, errors.New("EVM driver requires atomic block committer")
	}
	if err := cfg.WatcherSettings.Validate(); err != nil {
		return nil, err
	}
	if !cfg.MaxFeeMinor.IsPositive() {
		return nil, errors.New("EVM driver max fee is required")
	}
	if cfg.QuoteTTL <= 0 {
		cfg.QuoteTTL = defaultQuoteTTL
	}
	if cfg.WatcherPollInterval <= 0 {
		cfg.WatcherPollInterval = 3 * time.Second
	}
	if cfg.WatcherLookback <= 0 {
		cfg.WatcherLookback = 64
	}
	return &Driver{network: cfg.Network, rpc: cfg.RPC, stateRepo: cfg.WatcherStateRepository, historyRepo: cfg.WatcherBlockHistory, holdRecorder: cfg.NetworkHoldRecorder,
		deriver: cfg.WalletDeriver, nonces: cfg.NonceManager, maxFee: cfg.MaxFeeMinor,
		quoteTTL: cfg.QuoteTTL, pollInterval: cfg.WatcherPollInterval, lookback: cfg.WatcherLookback, watcherSettings: cfg.WatcherSettings.Defaults()}, nil
}

func (d *Driver) Network() crypto.NetworkRef                   { return d.network }
func (d *Driver) Capabilities() crypto.ChainFamilyCapabilities { return evmCapabilities() }
func (d *Driver) Watchers() crypto.WatcherFactory              { return d }
func (d *Driver) Transactions() crypto.TransactionBuilder      { return d }
func (d *Driver) Broadcaster() crypto.Broadcaster              { return d }
func (d *Driver) Finality() crypto.FinalityReader              { return d }
func (d *Driver) Balances() crypto.BalanceReader               { return d }
func (d *Driver) Wallets() crypto.WalletDeriver                { return d }
func (d *Driver) Accounts() crypto.AccountLifecycle            { return nil }
func (d *Driver) TokenAccounts() crypto.TokenAccountLifecycle  { return nil }
func (d *Driver) Addresses() crypto.AddressCodec               { return d }

func (d *Driver) RuntimeEndpoints() []crypto.RuntimeEndpointStatus { return d.rpc.Status() }
func (d *Driver) ProbeRuntime(ctx context.Context)                 { d.rpc.Probe(ctx) }

func (d *Driver) NewNativeWatcher(_ context.Context, network crypto.NetworkRef, addresses []string) (crypto.ManagedWatcher, error) {
	if err := d.validateNetwork(network); err != nil {
		return nil, err
	}
	return NewWatcher(WatcherConfig{Network: network, RPC: d.rpc, StateRepository: d.stateRepo,
		BlockHistoryRepository: d.historyRepo, NetworkHoldRecorder: d.holdRecorder,
		WatcherKey: "native_eth", Addresses: addresses, PollInterval: d.pollInterval, Lookback: d.lookback, Settings: d.watcherSettings})
}

func (d *Driver) NewTokenWatcher(_ context.Context, network crypto.NetworkRef, asset crypto.AssetRef, addresses []string) (crypto.ManagedWatcher, error) {
	if err := d.validateNetwork(network); err != nil {
		return nil, err
	}
	if !asset.Standard.IsEVMContract() {
		return nil, fmt.Errorf("EVM token watcher requires an EVM contract token, got %s", asset.Standard)
	}
	contract, err := NormalizeAddress(asset.ContractAddress)
	if err != nil {
		return nil, fmt.Errorf("EVM token contract: %w", err)
	}
	asset.ContractAddress = contract
	return NewWatcher(WatcherConfig{Network: network, RPC: d.rpc, StateRepository: d.stateRepo,
		BlockHistoryRepository: d.historyRepo, NetworkHoldRecorder: d.holdRecorder,
		WatcherKey: "erc20:" + strings.ToLower(contract), Asset: &asset, Addresses: addresses,
		PollInterval: d.pollInterval, Lookback: d.lookback, Settings: d.watcherSettings})
}

type feeEvidence struct {
	GasEstimate             uint64 `json:"gasEstimate"`
	GasLimit                uint64 `json:"gasLimit"`
	BaseFeePerGasWei        string `json:"baseFeePerGasWei"`
	MaxPriorityFeePerGasWei string `json:"maxPriorityFeePerGasWei"`
	MaxFeePerGasWei         string `json:"maxFeePerGasWei"`
}

func (d *Driver) QuoteFee(ctx context.Context, intent crypto.TransferIntent) (crypto.FeeQuote, error) {
	if err := d.validateIntent(intent); err != nil {
		return crypto.FeeQuote{}, err
	}
	message, err := d.callMessage(intent)
	if err != nil {
		return crypto.FeeQuote{}, err
	}
	gasEstimate, err := d.rpc.EstimateGas(ctx, message)
	if err != nil {
		return crypto.FeeQuote{}, fmt.Errorf("estimate EVM gas: %w", err)
	}
	gasLimit := gasEstimate + (gasEstimate+4)/5
	header, err := d.rpc.HeaderByNumber(ctx, nil)
	if err != nil || header.BaseFee == nil || header.BaseFee.Sign() <= 0 {
		return crypto.FeeQuote{}, fmt.Errorf("read EVM base fee: %w", err)
	}
	tip, err := d.suggestTip(ctx)
	if err != nil {
		return crypto.FeeQuote{}, err
	}
	maxFeePerGas := new(big.Int).Add(new(big.Int).Mul(header.BaseFee, big.NewInt(2)), tip)
	expectedGasPrice := new(big.Int).Add(header.BaseFee, tip)
	if expectedGasPrice.Cmp(maxFeePerGas) > 0 {
		expectedGasPrice.Set(maxFeePerGas)
	}
	estimated, _ := types.NewRawAmountFromBigInt(new(big.Int).Mul(new(big.Int).SetUint64(gasEstimate), expectedGasPrice))
	maximum, _ := types.NewRawAmountFromBigInt(new(big.Int).Mul(new(big.Int).SetUint64(gasLimit), maxFeePerGas))
	if maximum.Cmp(d.maxFee) > 0 {
		return crypto.FeeQuote{}, fmt.Errorf("EVM maximum fee %s exceeds driver safety ceiling %s", maximum, d.maxFee)
	}
	evidence, _ := json.Marshal(feeEvidence{GasEstimate: gasEstimate, GasLimit: gasLimit,
		BaseFeePerGasWei: header.BaseFee.String(), MaxPriorityFeePerGasWei: tip.String(), MaxFeePerGasWei: maxFeePerGas.String()})
	return crypto.FeeQuote{EstimatedFee: estimated, MaxFee: maximum,
		NativeAsset: crypto.AssetRef{TokenID: d.network.NativeTokenID, Standard: token.TokenStandardNative, Decimals: d.network.NativeDecimals},
		Payer:       crypto.FeePayerSourceWallet, ExpiresAt: time.Now().UTC().Add(d.quoteTTL), RawEvidence: evidence}, nil
}

func (d *Driver) suggestTip(ctx context.Context) (*big.Int, error) {
	history, err := d.rpc.FeeHistory(ctx, 5, []float64{50})
	if err == nil {
		for _, v := range slices.Backward(history.Reward) {
			if len(v) > 0 && v[0] != nil && v[0].Sign() > 0 {
				return new(big.Int).Set(v[0]), nil
			}
		}
	}
	tip, fallbackErr := d.rpc.SuggestGasTipCap(ctx)
	if fallbackErr != nil || tip == nil || tip.Sign() <= 0 {
		return nil, fmt.Errorf("resolve EVM priority fee: %w", errors.Join(err, fallbackErr))
	}
	return tip, nil
}

type unsignedPayload struct {
	ChainID                 string               `json:"chainId"`
	Nonce                   string               `json:"nonce"`
	MaxPriorityFeePerGasWei string               `json:"maxPriorityFeePerGasWei"`
	MaxFeePerGasWei         string               `json:"maxFeePerGasWei"`
	GasLimit                uint64               `json:"gasLimit"`
	To                      string               `json:"to"`
	ValueWei                string               `json:"valueWei"`
	Data                    string               `json:"data"`
	AccessList              gethtypes.AccessList `json:"accessList"`
}

type signedPayload struct {
	RawTransaction string `json:"rawTransaction"`
}

func (d *Driver) Build(ctx context.Context, intent crypto.TransferIntent, quote crypto.FeeQuote) (crypto.UnsignedTxEnvelope, error) {
	if err := d.validateIntent(intent); err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	fees, err := d.validateQuote(quote)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	pendingNonce, err := d.rpc.PendingNonceAt(ctx, common.HexToAddress(intent.SourceAddress))
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, fmt.Errorf("read EVM pending nonce: %w", err)
	}
	lease, err := d.nonces.Reserve(ctx, NonceRequest{NetworkID: d.network.ID, SourceAddress: intent.SourceAddress,
		BasisType: intent.BasisType, BasisID: intent.BasisID, Purpose: intent.Purpose, Attempt: intent.Attempt,
		Generation: intent.EffectiveGeneration(), PendingNonce: pendingNonce})
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	return d.buildEnvelope(intent, lease.Nonce, lease.Generation, fees)
}

func (d *Driver) buildEnvelope(intent crypto.TransferIntent, nonce uint64, generation int, fees feeEvidence) (crypto.UnsignedTxEnvelope, error) {
	to, value, data, err := d.transactionSemantics(intent)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	payload := unsignedPayload{ChainID: d.network.NetworkIdentifier, Nonce: strconv.FormatUint(nonce, 10),
		MaxPriorityFeePerGasWei: fees.MaxPriorityFeePerGasWei, MaxFeePerGasWei: fees.MaxFeePerGasWei,
		GasLimit: fees.GasLimit, To: to.Hex(), ValueWei: value.String(), Data: "0x" + hex.EncodeToString(data), AccessList: gethtypes.AccessList{}}
	raw, err := json.Marshal(payload)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	fingerprint, err := crypto.CanonicalUnsignedFingerprint(crypto.ChainFamilyEVM, evmEnvelopeSchemaVersion, raw)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, err
	}
	return crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: evmEnvelopeSchemaVersion,
		UnsignedFingerprint: fingerprint, Generation: generation, NetworkSequence: payload.Nonce, Payload: raw}, nil
}

func (d *Driver) BuildReplacement(ctx context.Context, intent crypto.TransferIntent, previous crypto.UnsignedTxEnvelope, policyMaxFee types.RawAmount) (crypto.UnsignedTxEnvelope, crypto.FeeQuote, error) {
	if previous.Family != crypto.ChainFamilyEVM || previous.SchemaVersion != evmEnvelopeSchemaVersion || previous.Generation <= 0 {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, errors.New("invalid EVM replacement predecessor")
	}
	if previous.Generation >= 3 {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, errors.New("EVM replacement generation limit reached")
	}
	var old unsignedPayload
	if err := json.Unmarshal(previous.Payload, &old); err != nil {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, err
	}
	oldNonce, err := parseUint64(old.Nonce)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, err
	}
	if old.ChainID != d.network.NetworkIdentifier || previous.NetworkSequence != old.Nonce || len(old.AccessList) != 0 {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, errors.New("EVM replacement predecessor identity mismatch")
	}
	to, value, data, err := d.transactionSemantics(intent)
	if err != nil || old.To != to.Hex() || old.ValueWei != value.String() || old.Data != "0x"+hex.EncodeToString(data) {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, errors.New("EVM replacement changes immutable transaction intent")
	}
	fresh, err := d.QuoteFee(ctx, intent)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, err
	}
	fees, err := d.validateQuote(fresh)
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, err
	}
	oldTip, ok := new(big.Int).SetString(old.MaxPriorityFeePerGasWei, 10)
	if !ok {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, errors.New("invalid previous EVM priority fee")
	}
	oldCap, ok := new(big.Int).SetString(old.MaxFeePerGasWei, 10)
	if !ok {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, errors.New("invalid previous EVM fee cap")
	}
	tip, _ := new(big.Int).SetString(fees.MaxPriorityFeePerGasWei, 10)
	cap, _ := new(big.Int).SetString(fees.MaxFeePerGasWei, 10)
	tip = maxBig(tip, bump125(oldTip))
	cap = maxBig(cap, bump125(oldCap))
	fees.MaxPriorityFeePerGasWei, fees.MaxFeePerGasWei, fees.GasLimit = tip.String(), cap.String(), old.GasLimit
	maximum, _ := types.NewRawAmountFromBigInt(new(big.Int).Mul(new(big.Int).SetUint64(old.GasLimit), cap))
	if maximum.Cmp(policyMaxFee) > 0 || maximum.Cmp(d.maxFee) > 0 {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, errors.New("EVM replacement exceeds fee policy")
	}
	intent.Generation = previous.Generation + 1
	pending, err := d.rpc.PendingNonceAt(ctx, common.HexToAddress(intent.SourceAddress))
	if err != nil {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, err
	}
	lease, err := d.nonces.Reserve(ctx, NonceRequest{NetworkID: d.network.ID, SourceAddress: intent.SourceAddress,
		BasisType: intent.BasisType, BasisID: intent.BasisID, Purpose: intent.Purpose, Attempt: intent.Attempt,
		Generation: intent.Generation, PendingNonce: pending})
	if err != nil || lease.Nonce != oldNonce {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, errors.Join(err, ErrNonceLeaseConflict)
	}
	envelope, err := d.buildEnvelope(intent, oldNonce, intent.Generation, fees)
	baseFee, baseFeeOK := new(big.Int).SetString(fees.BaseFeePerGasWei, 10)
	if !baseFeeOK {
		return crypto.UnsignedTxEnvelope{}, crypto.FeeQuote{}, errors.New("invalid replacement base fee")
	}
	expectedPrice := new(big.Int).Add(baseFee, tip)
	if expectedPrice.Cmp(cap) > 0 {
		expectedPrice = new(big.Int).Set(cap)
	}
	fresh.EstimatedFee, _ = types.NewRawAmountFromBigInt(new(big.Int).Mul(new(big.Int).SetUint64(fees.GasEstimate), expectedPrice))
	fresh.MaxFee = maximum
	fresh.RawEvidence, _ = json.Marshal(fees)
	return envelope, fresh, err
}

func bump125(value *big.Int) *big.Int {
	result := new(big.Int).Mul(value, big.NewInt(1125))
	result.Add(result, big.NewInt(999))
	return result.Div(result, big.NewInt(1000))
}

func maxBig(a, b *big.Int) *big.Int {
	if a.Cmp(b) >= 0 {
		return new(big.Int).Set(a)
	}
	return new(big.Int).Set(b)
}

func (d *Driver) validateQuote(quote crypto.FeeQuote) (feeEvidence, error) {
	var evidence feeEvidence
	if quote.ExpiresAt.IsZero() || !time.Now().UTC().Before(quote.ExpiresAt) || !quote.MaxFee.IsPositive() || quote.MaxFee.Cmp(d.maxFee) > 0 {
		return evidence, errors.New("EVM fee quote is expired or outside driver policy")
	}
	if err := json.Unmarshal(quote.RawEvidence, &evidence); err != nil || evidence.GasEstimate == 0 || evidence.GasLimit < evidence.GasEstimate {
		return evidence, errors.New("EVM fee quote evidence is invalid")
	}
	baseFee, baseOK := new(big.Int).SetString(evidence.BaseFeePerGasWei, 10)
	tip, tipOK := new(big.Int).SetString(evidence.MaxPriorityFeePerGasWei, 10)
	cap, capOK := new(big.Int).SetString(evidence.MaxFeePerGasWei, 10)
	if !baseOK || !tipOK || !capOK || baseFee.Sign() <= 0 || tip.Sign() <= 0 || cap.Sign() <= 0 || tip.Cmp(cap) > 0 {
		return evidence, errors.New("EVM fee quote evidence contains invalid fee values")
	}
	maximum, _ := types.NewRawAmountFromBigInt(new(big.Int).Mul(new(big.Int).SetUint64(evidence.GasLimit), cap))
	expectedPrice := new(big.Int).Add(baseFee, tip)
	if expectedPrice.Cmp(cap) > 0 {
		expectedPrice = new(big.Int).Set(cap)
	}
	estimated, _ := types.NewRawAmountFromBigInt(new(big.Int).Mul(new(big.Int).SetUint64(evidence.GasEstimate), expectedPrice))
	if !maximum.Equal(quote.MaxFee) || !estimated.Equal(quote.EstimatedFee) {
		return evidence, errors.New("EVM fee quote totals do not match immutable evidence")
	}
	return evidence, nil
}

func (d *Driver) callMessage(intent crypto.TransferIntent) (ethereum.CallMsg, error) {
	to, value, data, err := d.transactionSemantics(intent)
	if err != nil {
		return ethereum.CallMsg{}, err
	}
	return ethereum.CallMsg{From: common.HexToAddress(intent.SourceAddress), To: &to, Value: value, Data: data}, nil
}

func (d *Driver) transactionSemantics(intent crypto.TransferIntent) (common.Address, *big.Int, []byte, error) {
	switch intent.Asset.Standard {
	case token.TokenStandardNative:
		return common.HexToAddress(intent.DestinationAddress), intent.Amount.BigInt(), nil, nil
	case token.TokenStandardERC20, token.TokenStandardBEP20:
		data, err := erc20ABI.Pack("transfer", common.HexToAddress(intent.DestinationAddress), intent.Amount.BigInt())
		return common.HexToAddress(intent.Asset.ContractAddress), new(big.Int), data, err
	default:
		return common.Address{}, nil, nil, fmt.Errorf("unsupported EVM asset standard %s", intent.Asset.Standard)
	}
}

func (d *Driver) Broadcast(ctx context.Context, envelope crypto.SignedTxEnvelope) (crypto.BroadcastOutcome, error) {
	if envelope.Family != crypto.ChainFamilyEVM || envelope.SchemaVersion != evmEnvelopeSchemaVersion || envelope.TxHash == "" {
		return crypto.BroadcastOutcome{}, errors.New("invalid signed EVM envelope")
	}
	var payload signedPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return crypto.BroadcastOutcome{}, err
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(payload.RawTransaction, "0x"))
	if err != nil {
		return crypto.BroadcastOutcome{}, errors.New("invalid signed EVM raw transaction")
	}
	var tx gethtypes.Transaction
	if err := tx.UnmarshalBinary(raw); err != nil {
		return crypto.BroadcastOutcome{}, fmt.Errorf("decode signed EVM transaction: %w", err)
	}
	if !strings.EqualFold(tx.Hash().Hex(), envelope.TxHash) || tx.ChainId().String() != d.network.NetworkIdentifier {
		return crypto.BroadcastOutcome{}, errors.New("signed EVM transaction identity mismatch")
	}
	sender, err := gethtypes.Sender(gethtypes.LatestSignerForChainID(tx.ChainId()), &tx)
	if err != nil {
		return crypto.BroadcastOutcome{}, fmt.Errorf("recover EVM sender: %w", err)
	}
	// The signed hash must become durable before the first network write. A
	// timeout can mean the provider accepted the bytes, so an ambiguous send is
	// never evidence that this nonce is reusable.
	if err := d.nonces.MarkSigned(ctx, d.network.ID, sender.Hex(), tx.Nonce(), envelope.Generation, tx.Hash().Hex()); err != nil {
		return crypto.BroadcastOutcome{}, fmt.Errorf("persist signed EVM nonce evidence: %w", err)
	}
	provider, sendErr := d.rpc.SendTransaction(ctx, &tx)
	if provider != "" {
		status := crypto.BroadcastAccepted
		if sendErr != nil {
			status = crypto.BroadcastAlreadyKnown
		}
		if err := d.nonces.MarkBroadcast(ctx, d.network.ID, sender.Hex(), tx.Nonce(), envelope.Generation, tx.Hash().Hex()); err != nil {
			return crypto.BroadcastOutcome{}, fmt.Errorf("persist EVM broadcast nonce evidence: %w", err)
		}
		return crypto.BroadcastOutcome{Status: status, TxHash: tx.Hash().Hex(), ProviderID: provider}, nil
	}
	if isDeterministicBroadcastRejection(sendErr) {
		return crypto.BroadcastOutcome{Status: crypto.BroadcastRejected, TxHash: tx.Hash().Hex(), Reason: sanitizedRPCError(sendErr)}, nil
	}
	return crypto.BroadcastOutcome{Status: crypto.BroadcastAmbiguous, TxHash: tx.Hash().Hex(), Reason: "RPC outcome unavailable"}, sendErr
}

func isDeterministicBroadcastRejection(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"invalid sender", "intrinsic gas too low", "insufficient funds", "transaction type not supported"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func (d *Driver) Receipt(ctx context.Context, txHash string) (crypto.ChainReceipt, error) {
	if !common.IsHexHash(txHash) {
		return crypto.ChainReceipt{}, errors.New("invalid EVM transaction hash")
	}
	receipt, err := d.rpc.TransactionReceipt(ctx, common.HexToHash(txHash))
	if errors.Is(err, ethereum.NotFound) {
		return crypto.ChainReceipt{NetworkID: d.network.ID, TxHash: txHash, Status: crypto.ReceiptPending,
			Canonicality: crypto.CanonicalityUnknown, Finality: crypto.FinalityProvisional, ObservedAt: time.Now().UTC()}, nil
	}
	if err != nil {
		return crypto.ChainReceipt{}, err
	}
	raw, _ := json.Marshal(receipt)
	result := crypto.ChainReceipt{NetworkID: d.network.ID, TxHash: receipt.TxHash.Hex(), Status: crypto.ReceiptSucceeded,
		BlockHash: receipt.BlockHash.Hex(), BlockHeight: receipt.BlockNumber.Int64(), Canonicality: crypto.CanonicalityCanonical,
		Finality: crypto.FinalityProvisional, RawReceipt: raw, ObservedAt: time.Now().UTC(),
		Fee: crypto.FeeSettlementReport{NativeAsset: crypto.AssetRef{TokenID: d.network.NativeTokenID, Standard: token.TokenStandardNative, Decimals: d.network.NativeDecimals}}}
	if receipt.Status == gethtypes.ReceiptStatusFailed {
		result.Status = crypto.ReceiptReverted
	}
	header, headerErr := d.rpc.CanonicalHeaderByNumber(ctx, receipt.BlockNumber)
	if headerErr != nil {
		return crypto.ChainReceipt{}, fmt.Errorf("read canonical EVM receipt block: %w", headerErr)
	}
	if header.Hash() != receipt.BlockHash {
		result.Canonicality = crypto.CanonicalityOrphaned
		return result, nil
	}
	latest, err := d.rpc.HeaderByNumber(ctx, nil)
	if err != nil {
		return crypto.ChainReceipt{}, err
	}
	if latest.Number.Cmp(receipt.BlockNumber) >= 0 {
		result.Confirmations = int(new(big.Int).Add(new(big.Int).Sub(latest.Number, receipt.BlockNumber), big.NewInt(1)).Int64())
	}
	var finalizedHeight int64 = -1
	if finalized, finalizedErr := d.rpc.HeaderByNumber(ctx, finalizedBlockNumber()); finalizedErr == nil {
		finalizedHeight = finalized.Number.Int64()
	}
	result.Finality = finalityFor(d.network, result.BlockHeight, result.Confirmations, finalizedHeight)
	price := receipt.EffectiveGasPrice
	if price == nil {
		price = new(big.Int)
	}
	total, _ := types.NewRawAmountFromBigInt(new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), price))
	result.Fee.TotalFee = total
	result.Fee.Components = []crypto.FeeComponent{{Name: "execution_gas", Amount: total, Quantity: int64(receipt.GasUsed)}}
	// A financially final receipt is replaced by its independent fee proof; a
	// proof that disagrees fails, and an unavailable quorum never settles a fee.
	if result.FinanciallyFinal() {
		if verified, proofErr := (&TransferVerifier{network: d.network, rpc: d.rpc}).VerifyNetworkFee(ctx, txHash); proofErr == nil {
			result = verified
		} else if errors.Is(proofErr, crypto.ErrVerificationMismatch) {
			return crypto.ChainReceipt{}, proofErr
		}
	}
	if err := d.nonces.MarkMined(ctx, d.network.ID, receipt.TxHash.Hex(), result.Finality != crypto.FinalityProvisional); err != nil {
		return crypto.ChainReceipt{}, fmt.Errorf("persist EVM mined nonce evidence: %w", err)
	}
	return result, nil
}

func finalityFor(network crypto.NetworkRef, blockHeight int64, confirmations int, finalizedHeight int64) crypto.FinalityStatus {
	byConfirmations := confirmations >= network.RequiredConfirmations
	byProtocol := finalizedHeight >= 0 && blockHeight <= finalizedHeight
	switch network.FinalityMode {
	case crypto.FinalityByProtocolFinalized:
		if byProtocol {
			return crypto.FinalityFinalized
		}
	case crypto.FinalityByConfirmationsOrHead:
		if byProtocol {
			return crypto.FinalityFinalized
		}
		if byConfirmations {
			return crypto.FinalitySolidified
		}
	default:
		if byConfirmations {
			return crypto.FinalitySolidified
		}
	}
	return crypto.FinalityProvisional
}

func (d *Driver) FinalizedBlock(ctx context.Context) (int64, string, error) {
	header, err := d.rpc.HeaderByNumber(ctx, finalizedBlockNumber())
	if err != nil {
		return 0, "", err
	}
	return header.Number.Int64(), header.Hash().Hex(), nil
}

func (d *Driver) CanonicalBlock(ctx context.Context, height int64) (string, error) {
	if height < 0 {
		return "", errors.New("EVM canonical block height cannot be negative")
	}
	header, err := d.rpc.CanonicalHeaderByNumber(ctx, big.NewInt(height))
	if err != nil {
		return "", err
	}
	return header.Hash().Hex(), nil
}

func (d *Driver) NativeBalance(ctx context.Context, address string) (types.RawAmount, error) {
	normalized, err := NormalizeAddress(address)
	if err != nil {
		return types.ZeroRawAmount(), err
	}
	balance, err := d.rpc.BalanceAt(ctx, common.HexToAddress(normalized))
	if err != nil {
		return types.ZeroRawAmount(), err
	}
	return types.NewRawAmountFromBigInt(balance)
}

func (d *Driver) TokenBalance(ctx context.Context, asset crypto.AssetRef, address string) (types.RawAmount, error) {
	if !asset.Standard.IsEVMContract() {
		return types.ZeroRawAmount(), errors.New("EVM token balance requires an EVM contract token")
	}
	normalized, err := NormalizeAddress(address)
	if err != nil {
		return types.ZeroRawAmount(), err
	}
	contract, err := NormalizeAddress(asset.ContractAddress)
	if err != nil {
		return types.ZeroRawAmount(), err
	}
	data, _ := erc20ABI.Pack("balanceOf", common.HexToAddress(normalized))
	to := common.HexToAddress(contract)
	output, err := d.rpc.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data})
	if err != nil {
		return types.ZeroRawAmount(), err
	}
	values, err := erc20ABI.Unpack("balanceOf", output)
	if err != nil || len(values) != 1 {
		return types.ZeroRawAmount(), errors.New("invalid ERC-20 balanceOf response")
	}
	balance, ok := values[0].(*big.Int)
	if !ok {
		return types.ZeroRawAmount(), errors.New("invalid ERC-20 balance type")
	}
	return types.NewRawAmountFromBigInt(balance)
}

func (d *Driver) DeriveAddress(ctx context.Context, network crypto.NetworkRef, derivationPath string) (crypto.WalletDerivation, error) {
	if err := d.validateNetwork(network); err != nil {
		return crypto.WalletDerivation{}, err
	}
	derived, err := d.deriver.DeriveAddress(ctx, network, derivationPath)
	if err != nil {
		return crypto.WalletDerivation{}, err
	}
	normalized, err := NormalizeAddress(derived.Address)
	if err != nil {
		return crypto.WalletDerivation{}, fmt.Errorf("vault returned invalid EVM address: %w", err)
	}
	derived.Address = normalized
	return derived, nil
}

func (d *Driver) Normalize(address string) (string, error) { return NormalizeAddress(address) }
func (d *Driver) Validate(address string) error            { return ValidateAddress(address) }

func (d *Driver) ValidateAsset(ctx context.Context, asset crypto.AssetRef) error {
	if asset.Standard == token.TokenStandardNative {
		return nil
	}
	if !asset.Standard.IsEVMContract() {
		return fmt.Errorf("unsupported EVM asset %s", asset.Standard)
	}
	contract, err := NormalizeAddress(asset.ContractAddress)
	if err != nil {
		return err
	}
	to := common.HexToAddress(contract)
	code, err := d.rpc.CodeAt(ctx, to)
	if err != nil {
		return fmt.Errorf("ERC-20 bytecode preflight: %w", err)
	}
	if len(code) == 0 {
		return errors.New("ERC-20 contract has no bytecode")
	}
	for _, method := range []string{"name", "symbol", "decimals"} {
		data, _ := erc20ABI.Pack(method)
		output, err := d.rpc.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data})
		if err != nil {
			return fmt.Errorf("ERC-20 %s preflight: %w", method, err)
		}
		values, err := erc20ABI.Unpack(method, output)
		if err != nil || len(values) != 1 {
			return fmt.Errorf("ERC-20 %s returned invalid data", method)
		}
		if method == "decimals" {
			decimals, ok := values[0].(uint8)
			if !ok || int(decimals) != asset.Decimals {
				return fmt.Errorf("ERC-20 decimals mismatch: chain=%v catalog=%d", values[0], asset.Decimals)
			}
		} else if value, ok := values[0].(string); !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("ERC-20 %s returned an empty or invalid string", method)
		} else if method == "symbol" && asset.Symbol != "" && value != asset.Symbol {
			return fmt.Errorf("ERC-20 symbol mismatch: chain=%q catalog=%q", value, asset.Symbol)
		} else if method == "name" && asset.Name != "" && value != asset.Name {
			return fmt.Errorf("ERC-20 name mismatch: chain=%q catalog=%q", value, asset.Name)
		}
	}
	return nil
}

func (d *Driver) Preflight(ctx context.Context, assets []crypto.AssetRef) error {
	if _, _, err := d.FinalizedBlock(ctx); err != nil {
		return fmt.Errorf("EVM finalized block tag is unsupported: %w", err)
	}
	if _, err := d.rpc.FeeHistory(ctx, 2, []float64{50}); err != nil {
		return fmt.Errorf("EVM fee history is unsupported: %w", err)
	}
	for _, asset := range assets {
		if err := d.ValidateAsset(ctx, asset); err != nil {
			return fmt.Errorf("validate asset %s: %w", asset.Code, err)
		}
	}
	return nil
}

func (d *Driver) ReconcileStartup(ctx context.Context) error {
	reconciler, ok := d.nonces.(StartupNonceManager)
	if !ok {
		return errors.New("EVM nonce manager does not implement startup reconciliation")
	}
	var divergences []NonceDivergence
	recoverer, recoverable := d.nonces.(StartupNonceRecoverer)
	if recoverable {
		report, err := recoverer.ReconcileStartupReport(ctx, d.network.ID, d)
		if err != nil {
			return err
		}
		divergences = report.Divergences
		if err := d.releaseVerifiedNonceHolds(ctx, recoverer, report.Verified); err != nil {
			return err
		}
	} else {
		var err error
		if divergences, err = reconciler.ReconcileStartup(ctx, d.network.ID, d); err != nil {
			return err
		}
	}
	for _, divergence := range divergences {
		if err := d.holdNonceDivergence(ctx, divergence); err != nil {
			return err
		}
	}
	if len(divergences) > 0 {
		return fmt.Errorf("EVM startup nonce reconciliation found %d divergence(s)", len(divergences))
	}
	return nil
}

// releaseVerifiedNonceHolds resolves the network hold before the cursor hold:
// an interrupted release leaves the cursor held, and the next startup repeats
// both idempotent steps.
func (d *Driver) releaseVerifiedNonceHolds(ctx context.Context, recoverer StartupNonceRecoverer, verified []VerifiedNonceHold) error {
	if len(verified) == 0 {
		return nil
	}
	releaser, ok := d.holdRecorder.(crypto.NetworkOperationalHoldReleaser)
	if !ok {
		return nil // Without network hold resolution the cursor stays held for an operator.
	}
	for _, hold := range verified {
		subject := map[string]string{"sourceAddress": hold.SourceAddress, "reason": NonceProviderDisagreement}
		if err := releaser.ReleaseNetworkHolds(ctx, d.network.ID, crypto.OperationalHoldNonceDivergence, subject,
			"provider disagreement re-verified: fresh provider quorum agrees with the fenced DB cursor"); err != nil {
			return fmt.Errorf("release nonce divergence network hold for %s: %w", hold.SourceAddress, err)
		}
		if err := recoverer.ReleaseVerifiedHold(ctx, d.network.ID, hold); err != nil {
			return err
		}
		logger.Info(ctx, "EVM nonce provider-disagreement hold released after re-verification", "network_id", d.network.ID, "source_address", hold.SourceAddress, "next_nonce", hold.NextNonce)
	}
	return nil
}

func (d *Driver) ObserveNonceState(ctx context.Context, sourceAddress string) (ProviderNonceSnapshot, error) {
	return d.rpc.ObserveNonceState(ctx, sourceAddress)
}

func (d *Driver) AssessReplacement(ctx context.Context, sourceAddress, networkSequence string, txHashes []string) (crypto.ReplacementAssessment, error) {
	source, err := NormalizeAddress(sourceAddress)
	if err != nil {
		return crypto.ReplacementAssessment{}, err
	}
	nonce, err := parseUint64(networkSequence)
	if err != nil || len(txHashes) == 0 {
		return crypto.ReplacementAssessment{}, errors.New("replacement assessment requires nonce and transaction hashes")
	}
	hashes := make([][32]byte, 0, len(txHashes))
	for _, value := range txHashes {
		if !common.IsHexHash(value) {
			return crypto.ReplacementAssessment{}, fmt.Errorf("invalid replacement transaction hash %q", value)
		}
		hashes = append(hashes, common.HexToHash(value))
	}
	return d.rpc.assessReplacement(ctx, common.HexToAddress(source), nonce, hashes), nil
}

// holdNonceDivergence keys the startup incident by the fenced DB cursor, so a
// divergence recurring after a re-verified release opens a new incident.
func (d *Driver) holdNonceDivergence(ctx context.Context, divergence NonceDivergence) error {
	if divergence.NextNonce == "" {
		return d.HoldNonceDivergence(ctx, divergence.SourceAddress, divergence.Reason)
	}
	if err := d.nonces.HoldSource(ctx, d.network.ID, divergence.SourceAddress, divergence.Reason); err != nil {
		return err
	}
	return d.holdRecorder.HoldNetwork(ctx, d.network.ID, crypto.OperationalHoldNonceDivergence,
		map[string]any{"sourceAddress": divergence.SourceAddress, "reason": divergence.Reason, "dbNextNonce": divergence.NextNonce})
}

func (d *Driver) HoldNonceDivergence(ctx context.Context, sourceAddress, reason string) error {
	if err := d.nonces.HoldSource(ctx, d.network.ID, sourceAddress, reason); err != nil {
		return err
	}
	return d.holdRecorder.HoldNetwork(ctx, d.network.ID, crypto.OperationalHoldNonceDivergence,
		map[string]any{"sourceAddress": sourceAddress, "reason": reason})
}

func (d *Driver) ReclaimUnsignedSequence(ctx context.Context, intent crypto.TransferIntent) error {
	manager, ok := d.nonces.(UnsignedTailNonceManager)
	if !ok {
		return errors.New("EVM nonce manager does not support unsigned tail reclaim")
	}
	return manager.ReclaimUnsignedTail(ctx, NonceRequest{NetworkID: d.network.ID, SourceAddress: intent.SourceAddress,
		BasisType: intent.BasisType, BasisID: intent.BasisID, Purpose: intent.Purpose, Attempt: intent.Attempt, Generation: intent.EffectiveGeneration()})
}

func (d *Driver) validateNetwork(network crypto.NetworkRef) error {
	if network.ID != d.network.ID || network.Family != crypto.ChainFamilyEVM || network.NetworkIdentifier != d.network.NetworkIdentifier {
		return errors.New("EVM driver network identity mismatch")
	}
	return nil
}

func (d *Driver) validateIntent(intent crypto.TransferIntent) error {
	if err := d.validateNetwork(intent.Network); err != nil {
		return err
	}
	if strings.TrimSpace(intent.BasisType) == "" || id.IsNil(intent.BasisID) || intent.Attempt <= 0 || !intent.Amount.IsPositive() {
		return errors.New("EVM transfer intent requires basis, positive attempt, and amount")
	}
	if _, err := NormalizeAddress(intent.SourceAddress); err != nil {
		return fmt.Errorf("EVM source: %w", err)
	}
	if _, err := NormalizeAddress(intent.DestinationAddress); err != nil {
		return fmt.Errorf("EVM destination: %w", err)
	}
	if intent.Asset.Standard.IsEVMContract() {
		if _, err := NormalizeAddress(intent.Asset.ContractAddress); err != nil {
			return fmt.Errorf("EVM contract: %w", err)
		}
	} else if intent.Asset.Standard != token.TokenStandardNative {
		return fmt.Errorf("unsupported EVM asset standard %s", intent.Asset.Standard)
	}
	return nil
}

func parseUint64(value string) (uint64, error) {
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok || !parsed.IsUint64() {
		return 0, errors.New("invalid uint64 decimal")
	}
	return parsed.Uint64(), nil
}

func evmCapabilities() crypto.ChainFamilyCapabilities {
	return crypto.ChainFamilyCapabilities{
		Family:               crypto.ChainFamilyEVM,
		WalletProvisioning:   crypto.WalletProvisioningDerived,
		RequiredRPCRoles:     []string{"rpc"},
		ExternalResourceMode: crypto.ExternalResourceNone,
		SequenceModel:        crypto.SequenceAccount,
		CheckpointTimeUnit:   crypto.CheckpointUnixSeconds,
		TokenAccountModel:    crypto.TokenAccountSameAddress,
		SupportsReplacement:  true,
	}
}

func (d *Driver) LagHints(string) crypto.WatcherLagHints {
	return crypto.WatcherLagHints{TimeUnit: crypto.CheckpointUnixSeconds, TracksHeadHeight: true}
}

var (
	_ crypto.NetworkDriver                 = (*Driver)(nil)
	_ crypto.DriverRuntimeReporter         = (*Driver)(nil)
	_ crypto.DriverRuntimeProber           = (*Driver)(nil)
	_ crypto.WatcherLagDescriptor          = (*Driver)(nil)
	_ crypto.ReplacementTransactionBuilder = (*Driver)(nil)
	_ crypto.AssetRuntimeValidator         = (*Driver)(nil)
	_ crypto.NetworkRuntimePreflighter     = (*Driver)(nil)
	_ crypto.CanonicalBlockReader          = (*Driver)(nil)
	_ crypto.ReplacementSafetyReader       = (*Driver)(nil)
	_ crypto.NonceDivergenceHandler        = (*Driver)(nil)
)
