package funding

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/crypto"
)

type fundingRPCStub struct {
	tx        *gethtypes.Transaction
	receipt   *gethtypes.Receipt
	canonical *gethtypes.Header
	latest    *gethtypes.Header
	finalized *gethtypes.Header
}

func (s fundingRPCStub) TransactionByHash(context.Context, [32]byte) (*gethtypes.Transaction, bool, error) {
	return s.tx, false, nil
}

func (s fundingRPCStub) TransactionReceipt(context.Context, [32]byte) (*gethtypes.Receipt, error) {
	return s.receipt, nil
}

func (s fundingRPCStub) CanonicalHeaderByNumber(context.Context, *big.Int) (*gethtypes.Header, error) {
	return s.canonical, nil
}

func (s fundingRPCStub) HeaderByNumber(_ context.Context, number *big.Int) (*gethtypes.Header, error) {
	if number != nil && number.Int64() == int64(gethrpc.FinalizedBlockNumber) {
		return s.finalized, nil
	}
	return s.latest, nil
}

func TestVerifyNativeTransferRequiresConfiguredFinality(t *testing.T) {
	chainID := big.NewInt(11155111)
	destination := common.HexToAddress("0x18D73e61c0e6714a6659681826fAcad2536113A3")
	tx := signedFundingTransaction(t, chainID, destination, big.NewInt(20_000_000_000_000_000))
	block := &gethtypes.Header{Number: big.NewInt(100), Time: 1_700_000_000}
	rpc := fundingRPCStub{
		tx: tx, canonical: block, latest: &gethtypes.Header{Number: big.NewInt(110)},
		finalized: &gethtypes.Header{Number: big.NewInt(99)},
		receipt:   &gethtypes.Receipt{Status: gethtypes.ReceiptStatusSuccessful, TxHash: tx.Hash(), BlockNumber: big.NewInt(100), BlockHash: block.Hash()},
	}
	target := fundingTarget{Address: destination.Hex(), TokenStandard: token.TokenStandardNative, requiredConfirmations: 12, finalityMode: crypto.FinalityByConfirmationsOrHead}

	_, err := verifyNativeTransfer(t.Context(), rpc, chainID, target, tx.Hash())
	require.ErrorContains(t, err, "is not final")

	rpc.latest = &gethtypes.Header{Number: big.NewInt(111)}
	transfer, err := verifyNativeTransfer(t.Context(), rpc, chainID, target, tx.Hash())
	require.NoError(t, err)
	require.Equal(t, 12, transfer.Confirmations)
	require.Equal(t, "20000000000000000", transfer.Amount.String())
}

func TestVerifyNativeTransferRejectsWrongDestination(t *testing.T) {
	chainID := big.NewInt(11155111)
	destination := common.HexToAddress("0x18D73e61c0e6714a6659681826fAcad2536113A3")
	tx := signedFundingTransaction(t, chainID, destination, big.NewInt(1))
	target := fundingTarget{Address: common.HexToAddress("0xCf85bA3cE269898f2fc25a6793a3F4d9cB2F784D").Hex(), TokenStandard: token.TokenStandardNative, requiredConfirmations: 12, finalityMode: crypto.FinalityByConfirmationsOrHead}

	_, err := verifyNativeTransfer(t.Context(), fundingRPCStub{tx: tx}, chainID, target, tx.Hash())
	require.ErrorContains(t, err, "is not a plain native transfer")
}

func signedFundingTransaction(t *testing.T, chainID *big.Int, destination common.Address, value *big.Int) *gethtypes.Transaction {
	t.Helper()
	key, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	tx := gethtypes.NewTx(&gethtypes.DynamicFeeTx{
		ChainID: chainID, Nonce: 1, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2),
		Gas: 21_000, To: &destination, Value: value,
	})
	signed, err := gethtypes.SignTx(tx, gethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	return signed
}
