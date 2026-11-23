package tron

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/providerpolicy"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/stretchr/testify/require"
)

const testFromHex = "411111111111111111111111111111111111111111"
const testToHex = "412222222222222222222222222222222222222222"
const testTokenHex = "413333333333333333333333333333333333333333"

var fixtureEpoch = time.Now().Add(-10 * time.Second).UnixMilli()

func blockFixture(t *testing.T, height int64, smart bool) (*BlockInfo, []TransactionInfo) {
	t.Helper()
	block := &BlockInfo{BlockID: fmt.Sprintf("%064x", height)}
	block.BlockHeader.RawData.Number = height
	block.BlockHeader.RawData.ParentHash = fmt.Sprintf("%064x", height-1)
	block.BlockHeader.RawData.Timestamp = fixtureEpoch + height
	contract := "TransferContract"
	value := `"to_address":"` + testToHex + `","amount":23`
	if smart {
		contract = "TriggerSmartContract"
		value = `"contract_address":"` + testToHex + `","call_value":7`
	}
	hash := sha256.Sum256([]byte{1})
	var tx AccountTransaction
	require.NoError(t, json.Unmarshal([]byte(`{"txID":"`+hex.EncodeToString(hash[:])+`","raw_data_hex":"01","ret":[{"contractRet":"SUCCESS"}],"raw_data":{"contract":[{"type":"`+contract+`","parameter":{"value":{"owner_address":"`+testFromHex+`",`+value+`}}}]}}`), &tx))
	block.Transactions = []AccountTransaction{tx}
	receipt := TransactionInfo{ID: tx.TxID, BlockNumber: height, BlockTimestamp: block.BlockHeader.RawData.Timestamp}
	if smart {
		receipt.Receipt.Result = "SUCCESS"
	}
	return block, []TransactionInfo{receipt}
}

func TestBlockDecoderRouterLogsAndStableInternalIndices(t *testing.T) {
	block, receipts := blockFixture(t, 10, true)
	fromTopic := strings.Repeat("0", 24) + testFromHex[2:]
	toTopic := strings.Repeat("0", 24) + testToHex[2:]
	receipts[0].Log = []TransactionLog{{Topics: []string{"other"}}, {Address: testTokenHex, Topics: []string{transferTopic, fromTopic, toTopic}, Data: fmt.Sprintf("%064x", 100)}, {Address: testTokenHex, Topics: []string{transferTopic, fromTopic, toTopic}, Data: fmt.Sprintf("%064x", 200)}}
	raw := `[{"hash":"` + strings.Repeat("a", 64) + `","caller_address":"` + testFromHex + `","transferTo_address":"` + testToHex + `","rejected":true,"note":"63616c6c","callValueInfo":[{"callValue":10}]},{"hash":"` + strings.Repeat("b", 64) + `","caller_address":"` + testFromHex + `","transferTo_address":"` + testToHex + `","note":"63616c6c","callValueInfo":[{"callValue":0},{"callValue":20,"tokenId":"1001"},{"callValue":30},{"callValue":40}]}]`
	require.NoError(t, json.Unmarshal([]byte(raw), &receipts[0].InternalTransactions))
	data, err := assembleBlock(block, receipts)
	require.NoError(t, err)
	events, err := decodeTransfers(crypto.NetworkRef{ID: id.New()}, data, crypto.FinalitySolidified, 21)
	require.NoError(t, err)
	require.Len(t, events, 5)
	require.Equal(t, crypto.EventKindNativeValue, events[0].EventKind)
	require.Equal(t, "7", events[0].Amount.String())
	require.Equal(t, int64(1), events[1].EventIndex)
	require.Equal(t, int64(2), events[2].EventIndex)
	require.Equal(t, NormalizeTronAddress(testTokenHex), events[1].TokenContract)
	require.Equal(t, int64(3), events[3].EventIndex)
	require.Equal(t, int64(4), events[4].EventIndex)
	require.Equal(t, crypto.EventKindInternalValue, events[3].EventKind)
	require.Equal(t, int64(1), *events[3].TraceIndex)
	require.Equal(t, int64(2), *events[3].CallValueIndex)
	require.Equal(t, strings.Repeat("b", 64), events[3].TraceHash)
	require.Equal(t, testTokenHex, receipts[0].Log[1].Address, "normalization must not mutate caller data")
}

func TestBlockCompletenessAndExecution(t *testing.T) {
	for _, scenario := range []string{"native", "missing", "duplicate", "foreign", "wrong_height", "unknown", "smart_pending", "revert", "raw_hash"} {
		t.Run(scenario, func(t *testing.T) {
			block, receipts := blockFixture(t, 10, scenario == "smart_pending" || scenario == "revert")
			var expected error
			switch scenario {
			case "missing":
				receipts = nil
				expected = crypto.ErrVerificationPending
			case "duplicate":
				receipts = append(receipts, receipts[0])
				expected = providerpolicy.ErrInvalidResponse
			case "foreign":
				receipts[0].ID = strings.Repeat("f", 64)
				expected = providerpolicy.ErrInvalidResponse
			case "wrong_height":
				receipts[0].BlockNumber++
				expected = providerpolicy.ErrInvalidResponse
			case "unknown":
				receipts[0].Receipt.Result = "NEW_SUCCESS_STATUS"
				expected = providerpolicy.ErrInvalidResponse
			case "smart_pending":
				receipts[0].Receipt.Result = ""
				expected = crypto.ErrVerificationPending
			case "revert":
				receipts[0].Receipt.Result = "REVERT"
			case "raw_hash":
				block.Transactions[0].RawDataHex = "02"
				expected = providerpolicy.ErrInvalidResponse
			}
			data, err := assembleBlock(block, receipts)
			if expected != nil {
				require.ErrorIs(t, err, expected)
				return
			}
			require.NoError(t, err)
			events, err := decodeTransfers(crypto.NetworkRef{ID: id.New()}, data, crypto.FinalitySolidified, 21)
			require.NoError(t, err)
			if scenario == "revert" {
				require.Empty(t, events)
			} else {
				require.Len(t, events, 1)
				require.Equal(t, "23", events[0].Amount.String())
			}
		})
	}
}
