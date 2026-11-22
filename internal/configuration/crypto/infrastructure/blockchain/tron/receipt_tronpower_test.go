package tron

import (
	"encoding/hex"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/json"
)

func TestCanonicalNile72026431RejectedTronPower(t *testing.T) {
	block, receipts := loadProfile(t, "nile-72026431")
	require.EqualValues(t, 72026431, block.BlockHeader.RawData.Number)
	require.Len(t, receipts, 8)
	before, err := json.Marshal(receipts)
	require.NoError(t, err)
	withoutTrace := slices.Clone(receipts)
	found := false
	for i, receipt := range receipts {
		if receipt.ID != "fe19adf1485422c34283ce14d273098674c190ad83da21c3f1bfe05c4bd08d75" {
			continue
		}
		found = true
		require.Len(t, receipt.InternalTransactions, 1)
		trace := receipt.InternalTransactions[0]
		require.True(t, trace.Rejected)
		require.Equal(t, hex.EncodeToString([]byte("freezeBalanceV2ForTronPower")), trace.Note)
		withoutTrace[i].InternalTransactions = nil
	}
	require.True(t, found)
	data, err := assembleBlock(block, receipts)
	require.NoError(t, err)
	require.Equal(t, []crypto.TraceExclusion{{Note: "freezeBalanceV2ForTronPower", Count: 1}}, data.ExcludedTraces)
	standard, err := assembleBlock(block, withoutTrace)
	require.NoError(t, err)
	require.Equal(t, standard.Digest, data.Digest)
	require.NotEqual(t, standard.SourceDigest, data.SourceDigest)
	events, err := decodeTransfers(crypto.NetworkRef{}, data, crypto.FinalitySolidified, 20)
	require.NoError(t, err)
	expected, err := decodeTransfers(crypto.NetworkRef{}, standard, crypto.FinalitySolidified, 20)
	require.NoError(t, err)
	require.Equal(t, expected, events)
	after, err := json.Marshal(receipts)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "normalization must not mutate source evidence")
}

func TestCanonicalTronPowerResourceShapeAndPaymentProjection(t *testing.T) {
	for _, note := range []string{"freezeBalanceV2ForTronPower", "unfreezeBalanceV2ForTronPower"} {
		t.Run(note, func(t *testing.T) {
			for _, rejected := range []bool{false, true} {
				block, receipts := blockFixture(t, 10, true)
				baseline, err := assembleBlock(block, receipts)
				require.NoError(t, err)
				trace := traceFixture(t, note, "b", 1_000_000)
				trace.TransferToAddress = trace.CallerAddress
				trace.Rejected = rejected
				receipts[0].InternalTransactions = []InternalTransaction{trace}
				data, err := assembleBlock(block, receipts)
				require.NoError(t, err)
				require.Equal(t, baseline.Digest, data.Digest)
				events, err := decodeTransfers(crypto.NetworkRef{}, data, crypto.FinalitySolidified, 20)
				require.NoError(t, err)
				require.Len(t, events, 1, "staking an owner's resources cannot create a counterparty payment")
			}
			for _, scenario := range []string{"receiver", "missing_value", "extra_value", "token", "negative_value"} {
				t.Run(scenario, func(t *testing.T) {
					trace := traceFixture(t, note, "b", 1_000_000)
					trace.TransferToAddress = trace.CallerAddress
					trace.Rejected = true
					switch scenario {
					case "receiver":
						trace.TransferToAddress = "412222222222222222222222222222222222222222"
					case "missing_value":
						trace.CallValueInfo = nil
					case "extra_value":
						trace.CallValueInfo = append(trace.CallValueInfo, trace.CallValueInfo[0])
					case "token":
						trace.CallValueInfo[0].TokenID = "1000001"
					case "negative_value":
						trace.CallValueInfo[0].CallValue = -1
					}
					_, _, err := classifyInternalTrace(trace)
					require.ErrorIs(t, err, ErrInvalidTrace)
				})
			}
		})
	}
}

func TestCanonicalRejectedUnknownResourceTraceStillStopsVerification(t *testing.T) {
	for _, note := range []string{"newOpcode", "freezeBalanceV2ForUnknownType", "freezeBalanceV2ForTronPowerSuffix"} {
		t.Run(note, func(t *testing.T) {
			trace := traceFixture(t, note, "b", 1_000_000)
			trace.TransferToAddress = trace.CallerAddress
			trace.Rejected = true
			_, _, err := classifyInternalTrace(trace)
			require.ErrorIs(t, err, ErrUnsupportedTrace)
			require.False(t, excludeFailedGroups(err, map[string]bool{}))
		})
	}
}
