package tron

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/providerpolicy"
)

const transferTopic = "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

type completeBlock struct {
	Block                *BlockInfo
	Receipts             []TransactionInfo // ordered by the block's transaction list
	Digest               string
	SourceDigest         string
	ExcludedTraces       []crypto.TraceExclusion
	OriginalTraceIndices [][]int
}

func validHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func validateBlock(block *BlockInfo, height int64) error {
	if block == nil || block.BlockHeader.RawData.Number != height || height < 0 || !validHash(block.BlockID) ||
		block.BlockHeader.RawData.Timestamp <= 0 || (height > 0 && !validHash(block.BlockHeader.RawData.ParentHash)) {
		return providerpolicy.ErrInvalidResponse
	}
	seen := make(map[string]bool, len(block.Transactions))
	for _, tx := range block.Transactions {
		if !validHash(tx.TxID) || seen[strings.ToLower(tx.TxID)] {
			return providerpolicy.ErrInvalidResponse
		}
		seen[strings.ToLower(tx.TxID)] = true
		// raw_data_hex authenticates the complete transaction, including fields
		// outside the projection needed by the transfer decoder.
		raw, err := hex.DecodeString(tx.RawDataHex)
		if err != nil || len(raw) == 0 {
			return providerpolicy.ErrInvalidResponse
		}
		hash := sha256.Sum256(raw)
		if !strings.EqualFold(tx.TxID, hex.EncodeToString(hash[:])) {
			return providerpolicy.ErrInvalidResponse
		}
	}
	return nil
}

func executionResult(tx AccountTransaction, info TransactionInfo) (crypto.ReceiptStatus, error) {
	failed := false
	classify := func(result string) error {
		switch strings.ToUpper(strings.TrimSpace(result)) {
		case "SUCCESS", "SUCESS":
		case "FAILED", "REVERT", "BAD_JUMP_DESTINATION", "OUT_OF_MEMORY", "PRECOMPILED_CONTRACT", "STACK_TOO_SMALL", "STACK_TOO_LARGE", "ILLEGAL_OPERATION", "STACK_OVERFLOW", "OUT_OF_ENERGY", "OUT_OF_TIME", "JVM_STACK_OVER_FLOW", "UNKNOWN", "TRANSFER_FAILED", "INVALID_CODE":
			failed = true
		default:
			return providerpolicy.ErrInvalidResponse
		}
		return nil
	}
	if len(tx.Ret) == 0 || len(tx.RawData.Contract) == 0 {
		return crypto.ReceiptPending, crypto.ErrVerificationPending
	}
	for _, ret := range tx.Ret {
		if err := classify(ret.ContractRet); err != nil {
			return crypto.ReceiptPending, err
		}
	}
	if info.Result != "" {
		if err := classify(info.Result); err != nil {
			return crypto.ReceiptPending, err
		}
	}
	smart := false
	for _, contract := range tx.RawData.Contract {
		if contract.Type == "TriggerSmartContract" || contract.Type == "CreateSmartContract" {
			smart = true
		}
	}
	if info.Receipt.Result != "" {
		if err := classify(info.Receipt.Result); err != nil {
			return crypto.ReceiptPending, err
		}
	} else if smart && !failed {
		return crypto.ReceiptPending, crypto.ErrVerificationPending
	}
	if failed {
		return crypto.ReceiptReverted, nil
	}
	return crypto.ReceiptSucceeded, nil
}

func assembleBlock(block *BlockInfo, receipts []TransactionInfo) (completeBlock, error) {
	if block == nil {
		return completeBlock{}, providerpolicy.ErrInvalidResponse
	}
	height := block.BlockHeader.RawData.Number
	if err := validateBlock(block, height); err != nil {
		return completeBlock{}, err
	}
	if len(receipts) > len(block.Transactions) {
		return completeBlock{}, providerpolicy.ErrInvalidResponse
	}
	if len(receipts) < len(block.Transactions) {
		return completeBlock{}, crypto.ErrVerificationPending
	}
	byID := make(map[string]TransactionInfo, len(receipts))
	for _, receipt := range receipts {
		key := strings.ToLower(receipt.ID)
		if !validHash(key) || receipt.BlockNumber != height || receipt.BlockTimestamp != block.BlockHeader.RawData.Timestamp ||
			(receipt.BlockHash != "" && !strings.EqualFold(receipt.BlockHash, block.BlockID)) {
			return completeBlock{}, providerpolicy.ErrInvalidResponse
		}
		if _, duplicate := byID[key]; duplicate {
			return completeBlock{}, providerpolicy.ErrInvalidResponse
		}
		byID[key] = receipt
	}
	normalized := *block
	normalized.BlockID = strings.ToLower(block.BlockID)
	normalized.BlockHeader.RawData.ParentHash = strings.ToLower(block.BlockHeader.RawData.ParentHash)
	normalized.Transactions = append([]AccountTransaction(nil), block.Transactions...)
	ordered := make([]TransactionInfo, 0, len(receipts))
	for i, tx := range normalized.Transactions {
		key := strings.ToLower(tx.TxID)
		receipt, exists := byID[key]
		if !exists {
			return completeBlock{}, providerpolicy.ErrInvalidResponse
		}
		if _, err := executionResult(tx, receipt); err != nil {
			return completeBlock{}, err
		}
		tx.TxID, tx.RawDataHex = key, strings.ToLower(tx.RawDataHex)
		tx.BlockNumber, tx.BlockTimestamp = height, block.BlockHeader.RawData.Timestamp
		tx.Ret = append(tx.Ret[:0:0], tx.Ret...)
		for j := range tx.Ret {
			tx.Ret[j].ContractRet = strings.ToUpper(tx.Ret[j].ContractRet)
		}
		normalized.Transactions[i] = tx
		receipt.ID, receipt.BlockHash = key, normalized.BlockID
		receipt.Result, receipt.Receipt.Result = strings.ToUpper(receipt.Result), strings.ToUpper(receipt.Receipt.Result)
		// Optional arrays are represented uniformly; array order determines event identity.
		logs := make([]TransactionLog, len(receipt.Log))
		copy(logs, receipt.Log)
		receipt.Log = logs
		traces := make([]InternalTransaction, len(receipt.InternalTransactions))
		copy(traces, receipt.InternalTransactions)
		receipt.InternalTransactions = traces
		for j := range receipt.Log {
			log := &receipt.Log[j]
			topics := make([]string, len(log.Topics))
			copy(topics, log.Topics)
			log.Topics = topics
			log.Address = NormalizeTronAddress(log.Address)
			log.Data = strings.ToLower(strings.TrimPrefix(log.Data, "0x"))
			for k := range log.Topics {
				log.Topics[k] = strings.ToLower(strings.TrimPrefix(log.Topics[k], "0x"))
			}
		}
		for j := range receipt.InternalTransactions {
			trace := &receipt.InternalTransactions[j]
			trace.Hash = strings.ToLower(trace.Hash)
			trace.CallerAddress, trace.TransferToAddress = NormalizeTronAddress(trace.CallerAddress), NormalizeTronAddress(trace.TransferToAddress)
			values := make([]struct {
				CallValue int64  `json:"callValue"`
				TokenID   string `json:"tokenId,omitempty"`
			}, len(trace.CallValueInfo))
			copy(values, trace.CallValueInfo)
			trace.CallValueInfo = values
		}
		ordered = append(ordered, receipt)
	}
	sourceRaw, err := json.Marshal(struct {
		Block    BlockInfo
		Receipts []TransactionInfo
	}{normalized, ordered})
	if err != nil {
		return completeBlock{}, err
	}
	sourceHash := sha256.Sum256(sourceRaw)
	data := completeBlock{Block: &normalized, Receipts: ordered, Digest: hex.EncodeToString(sourceHash[:])}
	data.SourceDigest = data.Digest
	data.Receipts, data.OriginalTraceIndices, data.ExcludedTraces, err = normalizeTransferReceipts(ordered)
	if err != nil {
		return completeBlock{}, err
	}
	raw, err := json.Marshal(struct {
		Policy   string
		Block    BlockInfo
		Receipts []TransactionInfo
	}{"tron-transfer-v2", normalized, data.Receipts})
	if err != nil {
		return completeBlock{}, err
	}
	hash := sha256.Sum256(raw)
	data.Digest = hex.EncodeToString(hash[:])
	return data, nil
}

// decodeTransfers operates on all successful transactions. A router's outer
// contract address does not limit which token emitters or traces are read.
func decodeTransfers(network crypto.NetworkRef, data completeBlock, finality crypto.FinalityStatus, confirmations int) ([]crypto.BlockchainEvent, error) {
	var events []crypto.BlockchainEvent
	for i, tx := range data.Block.Transactions {
		receipt := data.Receipts[i]
		status, err := executionResult(tx, receipt)
		if err != nil {
			return nil, err
		}
		if status != crypto.ReceiptSucceeded {
			continue
		}
		base := crypto.BlockchainEvent{NetworkID: network.ID, TxHash: tx.TxID,
			BlockNumber: data.Block.BlockHeader.RawData.Number, BlockHash: data.Block.BlockID,
			Timestamp: time.UnixMilli(data.Block.BlockHeader.RawData.Timestamp).UTC(),
			Finality:  finality, Canonicality: crypto.CanonicalityCanonical, Confirmations: confirmations,
			RequiredConfs: network.RequiredConfirmations, EventType: crypto.EventTypeTransfer}
		base.DecoderVersion = crypto.TRONTransferEvidenceSchema
		for index, contract := range tx.RawData.Contract {
			value := contract.Parameter.Value
			amount, to := value.Amount, value.ToAddress
			switch contract.Type {
			case "TransferContract":
			case "TriggerSmartContract":
				amount, to = value.CallValue, value.ContractAddress
			default:
				continue
			}
			if amount <= 0 {
				continue
			}
			event := base
			event.EventKind, event.EventIndex = crypto.EventKindNativeValue, int64(index)
			event.FromAddress, event.ToAddress = NormalizeTronAddress(value.OwnerAddress), NormalizeTronAddress(to)
			event.Amount = types.NewRawAmountFromInt64(amount)
			if err := validateTransferAddresses(event); err != nil {
				return nil, err
			}
			events = append(events, event)
		}
		for index, log := range receipt.Log {
			if len(log.Topics) != 3 || log.Topics[0] != transferTopic {
				continue
			}
			if !validHash(log.Data) || !validAddressTopic(log.Topics[1]) || !validAddressTopic(log.Topics[2]) {
				return nil, providerpolicy.ErrInvalidResponse
			}
			value, ok := new(big.Int).SetString(log.Data, 16)
			if !ok || value.Sign() <= 0 {
				continue
			}
			amount, err := types.NewRawAmountFromBigInt(value)
			if err != nil {
				return nil, err
			}
			event := base
			event.EventKind, event.EventIndex = crypto.EventKindContractLog, int64(index)
			event.TokenContract = NormalizeTronAddress(log.Address)
			event.FromAddress, event.ToAddress = NormalizeTronAddress(log.Topics[1][24:]), NormalizeTronAddress(log.Topics[2][24:])
			event.Amount = amount
			if err := validateTransferAddresses(event); err != nil {
				return nil, err
			}
			events = append(events, event)
		}
		var index int64
		for traceIndex, trace := range receipt.InternalTransactions {
			for callIndex, value := range trace.CallValueInfo {
				identity := index
				index++ // advance before rejecting TRC10, zero values or reverted calls
				if trace.Rejected || value.TokenID != "" || value.CallValue <= 0 {
					continue
				}
				event := base
				event.EventKind, event.EventIndex = crypto.EventKindInternalValue, identity
				event.TraceHash = trace.Hash
				tracePosition, callPosition := int64(data.OriginalTraceIndices[i][traceIndex]), int64(callIndex)
				event.TraceIndex, event.CallValueIndex = &tracePosition, &callPosition
				event.FromAddress, event.ToAddress = NormalizeTronAddress(trace.CallerAddress), NormalizeTronAddress(trace.TransferToAddress)
				event.Amount = types.NewRawAmountFromInt64(value.CallValue)
				if !validHash(trace.Hash) {
					return nil, providerpolicy.ErrInvalidResponse
				}
				if err := validateTransferAddresses(event); err != nil {
					return nil, err
				}
				events = append(events, event)
			}
		}
	}
	return events, nil
}

func validAddressTopic(topic string) bool {
	return validHash(topic) && topic[:24] == strings.Repeat("0", 24)
}

func validateTransferAddresses(event crypto.BlockchainEvent) error {
	for _, address := range []string{event.FromAddress, event.ToAddress} {
		if _, err := tronAddressToABIBytes(address); err != nil {
			return fmt.Errorf("invalid decoded transfer address: %w", providerpolicy.ErrInvalidResponse)
		}
	}
	if event.TokenContract != "" {
		if _, err := tronAddressToABIBytes(event.TokenContract); err != nil {
			return providerpolicy.ErrInvalidResponse
		}
	}
	return nil
}
