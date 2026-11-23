package tron

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/stretchr/testify/require"
)

func TestSemanticFailureRemainsVisibleInsideQuorumError(t *testing.T) {
	for _, tc := range []struct {
		err      error
		category string
	}{{ErrUnsupportedTrace, "unsupported_trace"}, {ErrInvalidTrace, "invalid_trace"}, {crypto.ErrDecoderVersionConflict, "decoder_version_conflict"}} {
		err := errors.Join(crypto.ErrChainQuorumUnavailable, tc.err)
		require.Equal(t, tc.category, blockErrorCategory(err))
		require.False(t, excludeFailedGroups(err, map[string]bool{}))
	}
}

func TestAuditedProtocolTraceFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/receipt_profiles/protocol-traces.json")
	require.NoError(t, err)
	var corpus struct {
		Cases []struct {
			Note    string              `json:"note"`
			Exclude bool                `json:"exclude"`
			Trace   InternalTransaction `json:"trace"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &corpus))
	require.Len(t, corpus.Cases, 24)
	for _, tc := range corpus.Cases {
		t.Run(tc.Note, func(t *testing.T) {
			note, excluded, err := classifyInternalTrace(tc.Trace)
			require.NoError(t, err)
			require.Equal(t, tc.Note, note)
			require.Equal(t, tc.Exclude, excluded)
			// Every note must still reject a malformed address; its spelling
			// alone cannot grant permission to discard arbitrary upstream data.
			tc.Trace.CallerAddress = "invalid"
			_, _, err = classifyInternalTrace(tc.Trace)
			require.ErrorIs(t, err, ErrInvalidTrace)
		})
	}
}

func loadProfile(t *testing.T, provider string) (*BlockInfo, []TransactionInfo) {
	t.Helper()
	root := "testdata/receipt_profiles"
	var block BlockInfo
	var receipts []TransactionInfo
	raw, err := os.ReadFile(filepath.Join(root, provider+"-getblockbynum.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &block))
	raw, err = os.ReadFile(filepath.Join(root, provider+"-gettransactioninfobyblocknum.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &receipts))
	return &block, receipts
}

func TestReceiptProfilesCanonicalAndSourceDigests(t *testing.T) {
	var canonical, firstSource string
	for _, provider := range []string{"trongrid", "nileex", "chainstack", "getblock"} {
		t.Run(provider, func(t *testing.T) {
			block, receipts := loadProfile(t, provider)
			before, err := json.Marshal(receipts)
			require.NoError(t, err)
			v2, err := assembleBlock(block, receipts)
			require.NoError(t, err)
			if canonical == "" {
				canonical = v2.Digest
				firstSource = v2.SourceDigest
			}
			require.Equal(t, canonical, v2.Digest)
			require.NotEmpty(t, v2.SourceDigest)
			require.Len(t, v2.Receipts[0].InternalTransactions, 9)
			if provider == "trongrid" || provider == "nileex" {
				require.Equal(t, firstSource, v2.SourceDigest)
				require.Len(t, v2.ExcludedTraces, 2)
			} else {
				require.NotEqual(t, firstSource, v2.SourceDigest)
				require.Empty(t, v2.ExcludedTraces)
			}
			after, err := json.Marshal(receipts)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after))
			_, err = decodeTransfers(crypto.NetworkRef{ID: id.New()}, v2, crypto.FinalitySolidified, 20)
			require.NoError(t, err)
		})
	}
}

func TestCanonicalPublicPositiveInternalTransfer(t *testing.T) {
	block, receipts := loadProfile(t, "positive-trongrid")
	data, err := assembleBlock(block, receipts)
	require.NoError(t, err)
	events, err := decodeTransfers(crypto.NetworkRef{}, data, crypto.FinalitySolidified, 20)
	require.NoError(t, err)
	var transfers []crypto.BlockchainEvent
	for _, event := range events {
		if event.EventKind == crypto.EventKindInternalValue {
			transfers = append(transfers, event)
		}
	}
	require.Len(t, transfers, 1)
	require.Equal(t, "dc0586ee1dfa586a9ebe8264926a9b905db1f269c517cea5d1274c59138a9675", transfers[0].TxHash)
	require.Equal(t, "ab77c81fa1e2b4570baf0dd3c5b7353a203ecb57a1e437d5120761fb5c31ee3d", transfers[0].TraceHash)
	require.Equal(t, "50000000", transfers[0].Amount.String())
}

func traceFixture(t *testing.T, note string, hashByte string, amount int64) InternalTransaction {
	t.Helper()
	var trace InternalTransaction
	raw := fmt.Sprintf(`{"hash":%q,"caller_address":%q,"transferTo_address":%q,"note":%q,"callValueInfo":[{"callValue":%d}]}`, strings.Repeat(hashByte, 64), testFromHex, testToHex, hex.EncodeToString([]byte(note)), amount)
	require.NoError(t, json.Unmarshal([]byte(raw), &trace))
	return trace
}

func TestCanonicalTransferEvidenceGolden(t *testing.T) {
	block, receipts := blockFixture(t, 10, false)
	block.BlockHeader.RawData.Timestamp = 1750000000010
	receipts[0].BlockTimestamp = block.BlockHeader.RawData.Timestamp
	metadata := traceFixture(t, "freezeBalanceV2ForEnergy", "a", 100)
	metadata.TransferToAddress = metadata.CallerAddress
	receipts[0].InternalTransactions = []InternalTransaction{metadata, traceFixture(t, "call", "b", 23)}
	data, err := assembleBlock(block, receipts)
	require.NoError(t, err)
	events, err := decodeTransfers(crypto.NetworkRef{ID: id.MustParse("01900000-0000-7000-8000-000000000001")}, data, crypto.FinalitySolidified, 20)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "90fe119abf3be637e5b034404774d68097668192313f6a909d0f26b14b445fd4", data.SourceDigest)
	require.Equal(t, "35c89be0f2736bd821ab634f60813d64e33142f903cad45993884d36aaffd361", data.Digest)
	require.Equal(t, "906f256f7bce8c1a3cefe727586d2f90901870debcf9d2fb95e1d4b8f9d1bed5", events[0].EconomicsHash())
	require.Equal(t, "9d1330e0a5fd63aac0b60b7215ffa9aa61ac70bc8d78fb43f9b6e81631911ebc", events[1].EconomicsHash())
}

func TestCanonicalMetadataDoesNotShiftOrCreateTransfer(t *testing.T) {
	block, receipts := blockFixture(t, 10, true)
	zero := traceFixture(t, "call", "a", 0)
	payment := traceFixture(t, "call", "b", 23)
	freeze := traceFixture(t, "freezeBalanceV2ForEnergy", "c", 1217309)
	freeze.TransferToAddress = freeze.CallerAddress
	receipts[0].InternalTransactions = []InternalTransaction{zero, payment}
	base, err := assembleBlock(block, receipts)
	require.NoError(t, err)
	receipts[0].InternalTransactions = []InternalTransaction{zero, freeze, payment}
	extended, err := assembleBlock(block, receipts)
	require.NoError(t, err)
	require.Equal(t, base.Digest, extended.Digest)
	var previous crypto.BlockchainEvent
	for i, data := range []completeBlock{base, extended} {
		events, err := decodeTransfers(crypto.NetworkRef{ID: id.ID{}}, data, crypto.FinalitySolidified, 20)
		require.NoError(t, err)
		require.Len(t, events, 2)
		event := events[1]
		require.Equal(t, "23", event.Amount.String())
		require.Equal(t, int64(1), event.EventIndex)
		require.Equal(t, 2, event.DecoderVersion)
		require.Equal(t, int64(i+1), *event.TraceIndex)
		if i > 0 {
			require.True(t, internalTransferIdentityEqual(event, previous))
			require.Equal(t, previous.EconomicsHash(), event.EconomicsHash())
		}
		previous = event
	}

}

func TestCanonicalInternalRankIncludesRequiredZeroRejectedAndTokenValues(t *testing.T) {
	block, receipts := blockFixture(t, 10, false)
	zero := traceFixture(t, "call", "a", 0)
	rejected := traceFixture(t, "call", "b", 99)
	rejected.Rejected = true
	trc10 := traceFixture(t, "call", "c", 11)
	trc10.CallValueInfo[0].TokenID = "1000001"
	payment := traceFixture(t, "call", "d", 23)
	v := payment.CallValueInfo[0]
	payment.CallValueInfo = append(payment.CallValueInfo, v, v, v)
	payment.CallValueInfo[0].CallValue = 0
	payment.CallValueInfo[1].TokenID = "1000001"
	payment.CallValueInfo[3].CallValue = 24
	metadata := traceFixture(t, "freezeBalanceV2ForEnergy", "e", 900)
	metadata.TransferToAddress = metadata.CallerAddress
	var hashes []string
	for _, includeMetadata := range []bool{false, true} {
		receipts[0].InternalTransactions = []InternalTransaction{zero, rejected, trc10}
		if includeMetadata {
			receipts[0].InternalTransactions = append(receipts[0].InternalTransactions, metadata)
		}
		receipts[0].InternalTransactions = append(receipts[0].InternalTransactions, payment)
		data, err := assembleBlock(block, receipts)
		require.NoError(t, err)
		events, err := decodeTransfers(crypto.NetworkRef{}, data, crypto.FinalitySolidified, 20)
		require.NoError(t, err)
		require.Len(t, events, 3)
		for i, event := range events[1:] {
			require.Equal(t, int64(5+i), event.EventIndex)
			require.Equal(t, int64(2+i), *event.CallValueIndex)
			if includeMetadata {
				require.Equal(t, int64(4), *event.TraceIndex)
				require.Equal(t, hashes[i], event.EconomicsHash())
			} else {
				require.Equal(t, int64(3), *event.TraceIndex)
				hashes = append(hashes, event.EconomicsHash())
			}
		}
	}
	require.NotEqual(t, hashes[0], hashes[1])
}

func TestCanonicalRejectsUnsupportedInvalidOrMissingRequiredTrace(t *testing.T) {
	for _, scenario := range []string{"unknown", "missing_note", "invalid_hex", "duplicate", "fake_vote_value", "fake_freeze_receiver", "financial_featured", "different_amount", "missing_call", "rejected_call"} {
		t.Run(scenario, func(t *testing.T) {
			block, receipts := blockFixture(t, 10, true)
			trace := traceFixture(t, "call", "a", 23)
			receipts[0].InternalTransactions = []InternalTransaction{trace}
			before, err := assembleBlock(block, receipts)
			require.NoError(t, err)
			switch scenario {
			case "unknown":
				receipts[0].InternalTransactions[0].Note = hex.EncodeToString([]byte("newOpcode"))
			case "missing_note":
				receipts[0].InternalTransactions[0].Note = ""
			case "invalid_hex":
				receipts[0].InternalTransactions[0].Note = "zz"
			case "duplicate":
				receipts[0].InternalTransactions = append(receipts[0].InternalTransactions, trace)
			case "fake_vote_value":
				receipts[0].InternalTransactions[0].Note = hex.EncodeToString([]byte("voteWitness"))
				receipts[0].InternalTransactions[0].TransferToAddress = ""
			case "fake_freeze_receiver":
				receipts[0].InternalTransactions[0].Note = hex.EncodeToString([]byte("freezeBalanceV2ForEnergy"))
			case "financial_featured":
				receipts[0].InternalTransactions[0].Note = hex.EncodeToString([]byte("withdrawExpireUnfreezeWhileSuiciding"))
			case "different_amount":
				receipts[0].InternalTransactions[0].CallValueInfo[0].CallValue++
			case "missing_call":
				receipts[0].InternalTransactions = nil
			case "rejected_call":
				receipts[0].InternalTransactions[0].Rejected = true
			}
			after, err := assembleBlock(block, receipts)
			switch scenario {
			case "different_amount", "missing_call", "rejected_call", "financial_featured":
				require.NoError(t, err)
				require.NotEqual(t, before.Digest, after.Digest)
			default:
				require.Error(t, err)
				require.True(t, semanticReadFailure(err))
				require.False(t, excludeFailedGroups(err, map[string]bool{}))
			}
		})
	}
}

func TestCanonicalResourceBeneficiaryIsNotPaymentAndSuicideReleaseIsPayment(t *testing.T) {
	block, receipts := blockFixture(t, 10, true)
	for _, note := range []string{"delegateResourceOfEnergy", "unfreezeForBandwidth"} {
		receipts[0].InternalTransactions = []InternalTransaction{traceFixture(t, note, "d", 123)}
		data, err := assembleBlock(block, receipts)
		require.NoError(t, err)
		events, err := decodeTransfers(crypto.NetworkRef{}, data, crypto.FinalitySolidified, 20)
		require.NoError(t, err)
		require.Len(t, events, 1, "resource beneficiary does not receive a TRX payment")
	}
	receipts[0].InternalTransactions = []InternalTransaction{traceFixture(t, "withdrawExpireUnfreezeWhileSuiciding", "d", 123)}
	data, err := assembleBlock(block, receipts)
	require.NoError(t, err)
	events, err := decodeTransfers(crypto.NetworkRef{}, data, crypto.FinalitySolidified, 20)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "123", events[1].Amount.String())
	receipts[0].InternalTransactions = nil
	missing, err := assembleBlock(block, receipts)
	require.NoError(t, err)
	require.NotEqual(t, data.Digest, missing.Digest, "a missing financial featured trace is a genuine mismatch")
}

func TestCanonicalUnknownTraceCannotBeHiddenByThirdProfile(t *testing.T) {
	block, receipts := blockFixture(t, 10, false)
	receipts[0].InternalTransactions = []InternalTransaction{traceFixture(t, "unknownOpcode", "a", 0)}
	var calls atomic.Int32
	first := rpcBlockServer(t, block, receipts, "", &calls)
	defer first.Close()
	_, standard := blockFixture(t, 10, false)
	second := rpcBlockServer(t, block, standard, "", &calls)
	defer second.Close()
	third := rpcBlockServer(t, block, standard, "", &calls)
	defer third.Close()
	pool := newTestRPCPool(t, []string{first.URL, second.URL, third.URL}, []string{first.URL, second.URL, third.URL})
	for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
		for i, endpoint := range pool.byRole[role] {
			endpoint.trustGroup = fmt.Sprint(i)
		}
	}
	_, _, err := pool.ReadAgreedBlock(t.Context(), 10)
	require.ErrorIs(t, err, ErrUnsupportedTrace)
	thirdEndpoint := pool.byRole[chainconfig.RoleSolidity][2]
	thirdEndpoint.mu.Lock()
	require.Empty(t, thirdEndpoint.operations)
	thirdEndpoint.mu.Unlock()
}
