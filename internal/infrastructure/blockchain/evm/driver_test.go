package evm

import (
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/crypto"
)

func TestFinalityUsesConfirmationsOrProtocolFinalized(t *testing.T) {
	network := crypto.NetworkRef{RequiredConfirmations: 12, FinalityMode: crypto.FinalityByConfirmationsOrHead}
	require.Equal(t, crypto.FinalityProvisional, finalityFor(network, 100, 11, 99))
	require.Equal(t, crypto.FinalitySolidified, finalityFor(network, 100, 12, 99))
	require.Equal(t, crypto.FinalityFinalized, finalityFor(network, 100, 1, 100))
}

func TestValidateQuoteRecomputesIntegerFeeTotals(t *testing.T) {
	driver := &Driver{maxFee: types.MustCryptoAmount("1000000")}
	evidence := feeEvidence{GasEstimate: 100, GasLimit: 120, BaseFeePerGasWei: "10", MaxPriorityFeePerGasWei: "2", MaxFeePerGasWei: "22"}
	raw, err := json.Marshal(evidence)
	require.NoError(t, err)
	quote := crypto.FeeQuote{EstimatedFee: types.MustCryptoAmount("1200"), MaxFee: types.MustCryptoAmount("2640"),
		ExpiresAt: time.Now().UTC().Add(time.Minute), RawEvidence: raw}
	_, err = driver.validateQuote(quote)
	require.NoError(t, err)

	quote.MaxFee = types.MustCryptoAmount("2639")
	_, err = driver.validateQuote(quote)
	require.ErrorContains(t, err, "totals")
}

func TestERC20SemanticsProduceExactTransferCalldata(t *testing.T) {
	driver := &Driver{}
	destination := "0x1111111111111111111111111111111111111111"
	intent := crypto.TransferIntent{Asset: crypto.AssetRef{Standard: token.TokenStandardERC20, ContractAddress: "0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB9"},
		DestinationAddress: destination, Amount: types.MustCryptoAmount("115792089237316195423570985008687907853269984665640564039457584007913129639935")}
	to, value, data, err := driver.transactionSemantics(intent)
	require.NoError(t, err)
	require.Equal(t, common.HexToAddress(intent.Asset.ContractAddress), to)
	require.Zero(t, value.Sign())
	require.Equal(t, "a9059cbb", hex.EncodeToString(data[:4]))
	decoded, err := erc20ABI.Methods["transfer"].Inputs.Unpack(data[4:])
	require.NoError(t, err)
	require.Equal(t, common.HexToAddress(destination), decoded[0])
	require.Equal(t, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)), decoded[1])
}

func TestReplacementRejectsAnyBusinessIntentMutationBeforeRPC(t *testing.T) {
	networkID, tokenID := id.New(), id.New()
	driver := &Driver{network: crypto.NetworkRef{ID: networkID, Code: "ETHEREUM-SEPOLIA", Family: crypto.ChainFamilyEVM,
		NetworkIdentifier: SepoliaChainIDForTest, NativeTokenID: tokenID, NativeDecimals: 18, RequiredConfirmations: 12,
		FinalityMode: crypto.FinalityByConfirmationsOrHead}}
	old := unsignedPayload{ChainID: SepoliaChainIDForTest, Nonce: "7", MaxPriorityFeePerGasWei: "100", MaxFeePerGasWei: "200", GasLimit: 21000,
		To: "0x1111111111111111111111111111111111111111", ValueWei: "10", Data: "0x", AccessList: gethtypes.AccessList{}}
	raw, err := json.Marshal(old)
	require.NoError(t, err)
	previous := crypto.UnsignedTxEnvelope{Family: crypto.ChainFamilyEVM, SchemaVersion: evmEnvelopeSchemaVersion, Generation: 1, NetworkSequence: "7", Payload: raw}
	intent := crypto.TransferIntent{BasisType: "CryptoSweepAttempt", BasisID: id.New(), Purpose: crypto.SigningPurposeSweep, Attempt: 1, Generation: 2,
		Network: driver.network, Asset: crypto.AssetRef{TokenID: tokenID, Standard: token.TokenStandardNative, Decimals: 18},
		SourceAddress: "0x2222222222222222222222222222222222222222", DestinationAddress: "0x3333333333333333333333333333333333333333", Amount: types.MustCryptoAmount("10")}
	_, _, err = driver.BuildReplacement(t.Context(), intent, previous, types.MustCryptoAmount("10000000"))
	require.ErrorContains(t, err, "changes immutable transaction intent")
}

func TestReplacementBumpRoundsUpToAtLeastTwelvePointFivePercent(t *testing.T) {
	require.Equal(t, "113", bump125(big.NewInt(100)).String())
	require.Equal(t, "126", bump125(big.NewInt(112)).String())
}

const SepoliaChainIDForTest = "11155111"

func TestEVMCapabilitiesAndLagHints(t *testing.T) {
	caps := evmCapabilities()
	require.NoError(t, caps.Validate())
	require.Equal(t, crypto.SequenceAccount, caps.SequenceModel)
	require.Equal(t, crypto.CheckpointUnixSeconds, caps.CheckpointTimeUnit)
	require.True(t, caps.SupportsReplacement)
	require.True(t, caps.RequiresNetworkSequence())

	driver := &Driver{}
	hints := driver.LagHints("native_eth")
	require.Equal(t, crypto.CheckpointUnixSeconds, hints.TimeUnit)
	require.Equal(t, time.Duration(0), hints.IndexingLag)
}
