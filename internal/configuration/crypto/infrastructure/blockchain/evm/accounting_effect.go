package evm

import (
	"math/big"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
)

// accountingIdentity locates the transfer in the complete successful receipt.
// Block-global log.Index remains an observation alias; its position in Logs is
// the transaction-local ordinal. Every log, including unrelated events, counts.
func accountingIdentity(event crypto.BlockchainEvent, receipt *gethtypes.Receipt) (crypto.AccountingEffectIdentity, error) {
	var empty crypto.AccountingEffectIdentity
	if receipt == nil || receipt.Status != gethtypes.ReceiptStatusSuccessful ||
		receipt.TxHash != common.HexToHash(event.TxHash) || receipt.BlockNumber == nil ||
		!receipt.BlockNumber.IsInt64() || receipt.BlockNumber.Int64() != event.BlockNumber ||
		receipt.BlockHash != common.HexToHash(event.BlockHash) {
		return empty, crypto.ErrVerificationMismatch
	}
	if event.EventKind == crypto.EventKindNativeValue {
		return crypto.NewAccountingEffectIdentity(event, crypto.ChainFamilyEVM, 0)
	}
	if event.EventKind != crypto.EventKindContractLog {
		return empty, crypto.ErrVerificationMismatch
	}
	ordinal := int64(-1)
	for index, log := range receipt.Logs {
		if log == nil || log.Removed || log.TxHash != receipt.TxHash || log.BlockHash != receipt.BlockHash ||
			log.BlockNumber != receipt.BlockNumber.Uint64() ||
			(index > 0 && (log.Index == 0 || receipt.Logs[index-1].Index != log.Index-1)) {
			return empty, crypto.ErrVerificationMismatch
		}
		if uint64(log.Index) != uint64(event.EventIndex) {
			continue
		}
		if log.Address != common.HexToAddress(event.TokenContract) || len(log.Topics) != 3 ||
			log.Topics[0] != erc20ABI.Events["Transfer"].ID || len(log.Data) != 32 ||
			common.BytesToAddress(log.Topics[1].Bytes()) != common.HexToAddress(event.FromAddress) ||
			common.BytesToAddress(log.Topics[2].Bytes()) != common.HexToAddress(event.ToAddress) ||
			new(big.Int).SetBytes(log.Data).Cmp(event.Amount.BigInt()) != 0 {
			return empty, crypto.ErrVerificationMismatch
		}
		ordinal = int64(index)
	}
	if ordinal < 0 {
		return empty, crypto.ErrVerificationMismatch
	}
	return crypto.NewAccountingEffectIdentity(event, crypto.ChainFamilyEVM, ordinal)
}
