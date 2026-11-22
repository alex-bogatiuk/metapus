package evm

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

func accountingReceipt(t *testing.T, blockIndex uint) (crypto.BlockchainEvent, *gethtypes.Receipt) {
	t.Helper()
	from, to, contract := common.HexToAddress("0x1"), common.HexToAddress("0x2"), common.HexToAddress("0x3")
	event := crypto.BlockchainEvent{
		NetworkID: id.MustParse("01900000-0000-7000-8000-000000000001"),
		TxHash:    "0x" + strings.Repeat("a", 64), BlockHash: "0x" + strings.Repeat("b", 64),
		EventKind: crypto.EventKindContractLog, EventIndex: int64(blockIndex + 2), BlockNumber: 100,
		FromAddress: from.Hex(), ToAddress: to.Hex(), TokenContract: contract.Hex(), Amount: types.MustRawAmount("12"),
	}
	receipt := &gethtypes.Receipt{
		Status: gethtypes.ReceiptStatusSuccessful, TxHash: common.HexToHash(event.TxHash),
		BlockHash: common.HexToHash(event.BlockHash), BlockNumber: big.NewInt(event.BlockNumber),
	}
	for ordinal := range 3 {
		log := &gethtypes.Log{
			Address: contract, TxHash: receipt.TxHash, BlockHash: receipt.BlockHash,
			BlockNumber: 100, Index: blockIndex + uint(ordinal),
			Topics: []common.Hash{common.HexToHash("0x123")},
		}
		if ordinal == 2 {
			log.Topics = []common.Hash{erc20ABI.Events["Transfer"].ID, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())}
			log.Data = common.LeftPadBytes(big.NewInt(12).Bytes(), 32)
		}
		receipt.Logs = append(receipt.Logs, log)
	}
	return event, receipt
}

func TestAccountingIdentityCountsUnrelatedReceiptLogsAndSurvivesReinclusion(t *testing.T) {
	event, receipt := accountingReceipt(t, 7)
	before, err := accountingIdentity(event, receipt)
	require.NoError(t, err)
	require.Equal(t, int64(2), before.Ordinal, "ordinal counts every receipt log, not only Transfer matches")
	reincluded, changedReceipt := accountingReceipt(t, 91)
	reincluded.BlockHash = "0x" + strings.Repeat("c", 64)
	changedReceipt.BlockHash = common.HexToHash(reincluded.BlockHash)
	for _, log := range changedReceipt.Logs {
		log.BlockHash = changedReceipt.BlockHash
	}
	after, err := accountingIdentity(reincluded, changedReceipt)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAccountingIdentityRejectsIncompleteOrConflictingReceipt(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*gethtypes.Receipt)
	}{
		{"nil log", func(r *gethtypes.Receipt) { r.Logs[0] = nil }},
		{"gap", func(r *gethtypes.Receipt) { r.Logs[1].Index++ }},
		{"reordered", func(r *gethtypes.Receipt) { r.Logs[0], r.Logs[1] = r.Logs[1], r.Logs[0] }},
		{"duplicate", func(r *gethtypes.Receipt) { r.Logs = append(r.Logs, r.Logs[2]) }},
		{"removed", func(r *gethtypes.Receipt) { r.Logs[0].Removed = true }},
		{"foreign transaction", func(r *gethtypes.Receipt) { r.Logs[0].TxHash = common.HexToHash("0xff") }},
		{"foreign block", func(r *gethtypes.Receipt) { r.Logs[0].BlockHash = common.HexToHash("0xff") }},
		{"wrong amount", func(r *gethtypes.Receipt) { r.Logs[2].Data = common.LeftPadBytes(big.NewInt(13).Bytes(), 32) }},
		{"missing target", func(r *gethtypes.Receipt) { r.Logs = r.Logs[:2] }},
		{"reverted", func(r *gethtypes.Receipt) { r.Status = gethtypes.ReceiptStatusFailed }},
	} {
		t.Run(test.name, func(t *testing.T) {
			event, receipt := accountingReceipt(t, 7)
			test.edit(receipt)
			_, err := accountingIdentity(event, receipt)
			require.Error(t, err)
		})
	}
}
