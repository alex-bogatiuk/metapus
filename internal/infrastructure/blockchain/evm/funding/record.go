package funding

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5"

	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/evm"
	chainfunding "metapus/internal/infrastructure/blockchain/funding"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

type rpcReader interface {
	TransactionByHash(context.Context, [32]byte) (*gethtypes.Transaction, bool, error)
	TransactionReceipt(context.Context, [32]byte) (*gethtypes.Receipt, error)
	CanonicalHeaderByNumber(context.Context, *big.Int) (*gethtypes.Header, error)
	HeaderByNumber(context.Context, *big.Int) (*gethtypes.Header, error)
}

type fundingTarget struct {
	chainfunding.Target
	network               crypto.NetworkRef
	requiredConfirmations int
	finalityMode          crypto.FinalityMode
}

// RecordConfirmedWalletFunding verifies a financially final, successful,
// top-level native EVM transfer before recording audited wallet funding.
func RecordConfirmedWalletFunding(
	ctx context.Context,
	rpc *evm.RPCPool,
	chainID *big.Int,
	walletCode string,
	tokenCode string,
	txHash string,
) (chainfunding.Result, error) {
	walletCode = strings.TrimSpace(walletCode)
	tokenCode = strings.TrimSpace(tokenCode)
	if walletCode == "" || tokenCode == "" {
		return chainfunding.Result{}, errors.New("wallet code and token code are required")
	}
	if rpc == nil || chainID == nil || chainID.Sign() <= 0 {
		return chainfunding.Result{}, errors.New("EVM RPC and positive chain ID are required")
	}
	hash, err := normalizeTransactionHash(txHash)
	if err != nil {
		return chainfunding.Result{}, err
	}

	target, err := loadTarget(ctx, walletCode, tokenCode)
	if err != nil {
		return chainfunding.Result{}, err
	}
	if target.TokenStandard != token.TokenStandardNative || strings.TrimSpace(target.ContractAddress) != "" {
		return chainfunding.Result{}, fmt.Errorf("EVM wallet funding currently accepts only the native token, got %s", target.TokenStandard)
	}
	if target.network.NetworkIdentifier != chainID.String() {
		return chainfunding.Result{}, crypto.ErrVerificationMismatch
	}

	transfer, err := verifyNativeTransfer(ctx, rpc, chainID, target, hash)
	if err != nil {
		return chainfunding.Result{}, err
	}
	verifier, err := evm.NewTransferVerifier(target.network, rpc)
	if err != nil {
		return chainfunding.Result{}, err
	}
	proof, err := verifier.VerifyTransfer(ctx, crypto.BlockchainEvent{
		NetworkID: target.NetworkID, TxHash: hash.Hex(), EventKind: crypto.EventKindNativeValue,
		FromAddress: transfer.FromAddress, ToAddress: transfer.ToAddress, Amount: transfer.Amount,
		BlockNumber: transfer.BlockNumber, BlockHash: transfer.BlockHash,
	})
	if err != nil {
		return chainfunding.Result{}, err
	}
	if proof.MatchingEffects != 1 {
		return chainfunding.Result{}, crypto.ErrVerificationPending
	}
	proofID, err := crypto_repo.NewBlockIngestionRepo().SaveVerification(ctx, proof)
	if err != nil {
		return chainfunding.Result{}, err
	}
	e := proof.Event
	return chainfunding.Record(ctx, walletCode, target.Target, e.TxHash, chainfunding.ConfirmedTransfer{
		VerificationID: &proofID, EventKind: e.EventKind, EventIndex: e.EventIndex,
		FromAddress: e.FromAddress, ToAddress: e.ToAddress, Amount: e.Amount,
		BlockNumber: e.BlockNumber, BlockHash: e.BlockHash, BlockTimestamp: e.Timestamp, Confirmations: e.Confirmations,
	})
}

func loadTarget(ctx context.Context, walletCode, tokenCode string) (fundingTarget, error) {
	txm := postgres.MustGetTxManager(ctx)
	var target fundingTarget
	var standard string
	var finalityMode string
	err := txm.GetQuerier(ctx).QueryRow(ctx, `
		SELECT w.id, w.network_id, t.id, t.code, t.token_standard,
		       COALESCE(t.contract_address, ''), w.address,
		       n.confirmations_needed, n.finality_mode,n.code,n.network_identifier,n.native_token_symbol,n.native_decimals
		FROM cat_wallets w
		JOIN cat_blockchain_networks n
		  ON n.id = w.network_id
		 AND n.chain_family = $3
		 AND n.is_active = TRUE
		 AND n.deletion_mark = FALSE
		JOIN cat_tokens t
		  ON t.network_id = w.network_id
		 AND t.code = $2
		 AND t.is_active = TRUE
		 AND t.deletion_mark = FALSE
		WHERE w.code = $1
		  AND w.is_active = TRUE
		  AND w.deletion_mark = FALSE
	`, walletCode, tokenCode, crypto.ChainFamilyEVM).Scan(
		&target.WalletID, &target.NetworkID, &target.TokenID, &target.TokenCode,
		&standard, &target.ContractAddress, &target.Address,
		&target.requiredConfirmations, &finalityMode,
		&target.network.Code, &target.network.NetworkIdentifier, &target.network.NativeTokenSymbol, &target.network.NativeDecimals,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return fundingTarget{}, fmt.Errorf("active EVM wallet %q with active token %q was not found", walletCode, tokenCode)
	}
	if err != nil {
		return fundingTarget{}, fmt.Errorf("load EVM wallet funding target: %w", err)
	}
	target.TokenStandard = token.TokenStandard(standard)
	target.finalityMode = crypto.FinalityMode(finalityMode)
	target.network.ID, target.network.NativeTokenID, target.network.Family = target.NetworkID, target.TokenID, crypto.ChainFamilyEVM
	target.network.RequiredConfirmations, target.network.FinalityMode = target.requiredConfirmations, target.finalityMode
	if target.requiredConfirmations < 1 || !target.finalityMode.Valid() {
		return fundingTarget{}, errors.New("EVM wallet funding target has invalid finality policy")
	}
	normalized, err := evm.NormalizeAddress(target.Address)
	if err != nil {
		return fundingTarget{}, fmt.Errorf("managed EVM wallet address: %w", err)
	}
	target.Address = normalized
	return target, nil
}

func verifyNativeTransfer(
	ctx context.Context,
	rpc rpcReader,
	chainID *big.Int,
	target fundingTarget,
	hash common.Hash,
) (chainfunding.ConfirmedTransfer, error) {
	tx, pending, err := rpc.TransactionByHash(ctx, hash)
	if errors.Is(err, ethereum.NotFound) {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("EVM transaction %s was not found", hash.Hex())
	}
	if err != nil {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("load EVM funding transaction: %w", err)
	}
	if pending {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("EVM transaction %s is still pending", hash.Hex())
	}
	if tx.Hash() != hash || tx.To() == nil || tx.To().Hex() != target.Address || tx.Value().Sign() <= 0 || len(tx.Data()) != 0 {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("transaction %s is not a plain native transfer into wallet %s", hash.Hex(), target.Address)
	}
	if !tx.Protected() || tx.ChainId() == nil || tx.ChainId().Cmp(chainID) != 0 {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("transaction %s is not replay-protected for chain ID %s", hash.Hex(), chainID.String())
	}
	from, err := gethtypes.Sender(gethtypes.LatestSignerForChainID(chainID), tx)
	if err != nil {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("recover EVM funding sender: %w", err)
	}

	receipt, err := rpc.TransactionReceipt(ctx, hash)
	if errors.Is(err, ethereum.NotFound) {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("EVM transaction %s has no receipt", hash.Hex())
	}
	if err != nil {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("load EVM funding receipt: %w", err)
	}
	if receipt.Status != gethtypes.ReceiptStatusSuccessful || receipt.BlockNumber == nil || receipt.BlockNumber.Sign() <= 0 || receipt.TxHash != hash {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("EVM funding transaction %s did not execute successfully", hash.Hex())
	}
	canonical, err := rpc.CanonicalHeaderByNumber(ctx, receipt.BlockNumber)
	if err != nil {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("verify canonical EVM funding block: %w", err)
	}
	if canonical.Hash() != receipt.BlockHash {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("EVM funding transaction %s is orphaned", hash.Hex())
	}
	latest, err := rpc.HeaderByNumber(ctx, nil)
	if err != nil {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("read latest EVM block: %w", err)
	}
	if latest.Number == nil || latest.Number.Cmp(receipt.BlockNumber) < 0 {
		return chainfunding.ConfirmedTransfer{}, errors.New("latest EVM head is behind the funding receipt")
	}
	confirmationBig := new(big.Int).Add(new(big.Int).Sub(latest.Number, receipt.BlockNumber), big.NewInt(1))
	if !confirmationBig.IsInt64() || confirmationBig.Int64() > int64(^uint(0)>>1) {
		return chainfunding.ConfirmedTransfer{}, errors.New("EVM funding confirmation count is out of range")
	}
	confirmations := int(confirmationBig.Int64())
	byConfirmations := confirmations >= target.requiredConfirmations
	byFinalized := false
	if finalized, finalizedErr := rpc.HeaderByNumber(ctx, big.NewInt(int64(gethrpc.FinalizedBlockNumber))); finalizedErr == nil && finalized.Number != nil {
		byFinalized = finalized.Number.Cmp(receipt.BlockNumber) >= 0
	}
	financiallyFinal := byConfirmations
	switch target.finalityMode {
	case crypto.FinalityByProtocolFinalized:
		financiallyFinal = byFinalized
	case crypto.FinalityByConfirmationsOrHead:
		financiallyFinal = byConfirmations || byFinalized
	}
	if !financiallyFinal {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf(
			"EVM funding transaction %s is not final: confirmations=%d required=%d finalized=%t",
			hash.Hex(), confirmations, target.requiredConfirmations, byFinalized,
		)
	}
	amount, err := types.NewRawAmountFromBigInt(tx.Value())
	if err != nil {
		return chainfunding.ConfirmedTransfer{}, fmt.Errorf("decode EVM funding amount: %w", err)
	}
	return chainfunding.ConfirmedTransfer{
		FromAddress: from.Hex(), ToAddress: target.Address, Amount: amount,
		EventKind: crypto.EventKindNativeValue, BlockHash: receipt.BlockHash.Hex(),
		BlockNumber: receipt.BlockNumber.Int64(), BlockTimestamp: time.Unix(int64(canonical.Time), 0).UTC(),
		Confirmations: confirmations,
	}, nil
}

func normalizeTransactionHash(value string) (common.Hash, error) {
	value = strings.TrimSpace(value)
	if len(value) != 66 || !strings.HasPrefix(value, "0x") || !common.IsHexHash(value) {
		return common.Hash{}, errors.New("EVM transaction hash must be 0x followed by 64 hexadecimal characters")
	}
	return common.HexToHash(value), nil
}
