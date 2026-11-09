package tron

import (
	"encoding/hex"
	"errors"
	"slices"
	"strings"

	"metapus/internal/domain/crypto"
)

var ErrUnsupportedTrace = errors.New("unsupported TRON internal transaction type")
var ErrInvalidTrace = errors.New("invalid TRON internal transaction shape")

// classifyInternalTrace never infers semantics from an operator's name. Unknown
// types stop the pair instead of disappearing via a less complete provider.
func classifyInternalTrace(trace InternalTransaction) (string, bool, error) {
	raw, err := hex.DecodeString(trace.Note)
	if err != nil || len(raw) == 0 {
		return "", false, ErrUnsupportedTrace
	}
	note := string(raw)
	if !validHash(trace.Hash) || ValidateTronAddress(NormalizeTronAddress(trace.CallerAddress)) != nil {
		return "", false, ErrInvalidTrace
	}
	for _, value := range trace.CallValueInfo {
		if value.CallValue < 0 {
			return "", false, ErrInvalidTrace
		}
	}
	switch note {
	case "call", "create", "suicide", "withdrawExpireUnfreezeWhileSuiciding":
		if ValidateTronAddress(NormalizeTronAddress(trace.TransferToAddress)) != nil {
			return "", false, ErrInvalidTrace
		}
		return note, false, nil
	case "voteWitness":
		if trace.TransferToAddress != "" {
			return "", false, ErrInvalidTrace
		}
		for _, value := range trace.CallValueInfo {
			if value.CallValue != 0 || value.TokenID != "" {
				return "", false, ErrInvalidTrace
			}
		}
		return note, true, nil
	// These operations change the caller's resources/own allowance, not a
	// merchant counterparty transfer. Keep the receiver-shape check explicit.
	case "freezeBalanceV2ForEnergy", "freezeBalanceV2ForBandwidth", "freezeBalanceV2ForTronPower",
		"unfreezeBalanceV2ForEnergy", "unfreezeBalanceV2ForBandwidth", "unfreezeBalanceV2ForTronPower",
		"withdrawReward", "withdrawExpireUnfreeze", "withdrawExpireUnfreezeWhileUnfreezing",
		"withdrawExpireUnfreezeWhileCanceling", "cancelAllUnfreezeV2":
		if trace.CallerAddress != trace.TransferToAddress || len(trace.CallValueInfo) != 1 || trace.CallValueInfo[0].TokenID != "" {
			return "", false, ErrInvalidTrace
		}
		return note, true, nil
	// Receiver here is a resource beneficiary. The corresponding native
	// processors adjust frozen/delegated balances; any release credits owner.
	case "freezeForEnergy", "freezeForBandwidth", "unfreezeForEnergy", "unfreezeForBandwidth",
		"delegateResourceOfEnergy", "delegateResourceOfBandwidth", "unDelegateResourceOfEnergy", "unDelegateResourceOfBandwidth":
		if ValidateTronAddress(NormalizeTronAddress(trace.TransferToAddress)) != nil || len(trace.CallValueInfo) != 1 || trace.CallValueInfo[0].TokenID != "" {
			return "", false, ErrInvalidTrace
		}
		return note, true, nil
	default:
		return "", false, ErrUnsupportedTrace
	}
}

// The returned arrays own their storage. Source coordinates stay outside the
// canonical projection; rejected, zero and TRC10 values keep their positions.
func normalizeTransferReceipts(receipts []TransactionInfo) ([]TransactionInfo, [][]int, []crypto.TraceExclusion, error) {
	out := append([]TransactionInfo(nil), receipts...)
	positions := make([][]int, len(receipts))
	counts := make(map[string]int)
	for i, receipt := range receipts {
		out[i].Log = slices.Clone(receipt.Log)
		for j := range out[i].Log {
			out[i].Log[j].Topics = slices.Clone(receipt.Log[j].Topics)
		}
		traces := make([]InternalTransaction, 0, len(receipt.InternalTransactions))
		positions[i] = make([]int, 0, len(receipt.InternalTransactions))
		seen := make(map[string]bool, len(receipt.InternalTransactions))
		for index, trace := range receipt.InternalTransactions {
			note, exclude, err := classifyInternalTrace(trace)
			if err != nil {
				return nil, nil, nil, err
			}
			if seen[trace.Hash] {
				return nil, nil, nil, ErrInvalidTrace
			}
			seen[trace.Hash] = true
			if exclude {
				counts[note]++
				continue
			}
			trace.Note = hex.EncodeToString([]byte(note))
			trace.CallValueInfo = slices.Clone(trace.CallValueInfo)
			traces = append(traces, trace)
			positions[i] = append(positions[i], index)
		}
		out[i].InternalTransactions = traces
	}
	notes := make([]string, 0, len(counts))
	for note := range counts {
		notes = append(notes, note)
	}
	slices.Sort(notes)
	excluded := make([]crypto.TraceExclusion, 0, len(notes))
	for _, note := range notes {
		excluded = append(excluded, crypto.TraceExclusion{Note: note, Count: counts[note]})
	}
	return out, positions, excluded, nil
}

func semanticReadFailure(err error) bool {
	return errors.Is(err, crypto.ErrDecoderVersionConflict) || errors.Is(err, ErrUnsupportedTrace) || errors.Is(err, ErrInvalidTrace)
}

func internalTransferIdentityEqual(a, b crypto.BlockchainEvent) bool {
	if a.EffectiveEventKind() != crypto.EventKindInternalValue || b.EffectiveEventKind() != crypto.EventKindInternalValue {
		return true
	}
	return a.TraceHash != "" && strings.EqualFold(a.TraceHash, b.TraceHash) && a.CallValueIndex != nil && b.CallValueIndex != nil && *a.CallValueIndex == *b.CallValueIndex
}
