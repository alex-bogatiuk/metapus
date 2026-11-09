package crypto

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAMLCoverageDistinguishesObservedEffectsFromCompleteEvidence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		kind     EventKind
		schema   int
		excluded bool
		status   CoverageStatus
		matching int
	}{
		{"indexed logs", EventKindContractLog, TransferEvidenceSchemaV1, false, CoverageComplete, 2},
		{"indexed native has no trace coverage", EventKindNativeValue, TransferEvidenceSchemaV1, false, CoverageUnknown, 0},
		{"canonical native", EventKindNativeValue, TRONTransferEvidenceSchema, false, CoverageComplete, 2},
		{"excluded traces", EventKindNativeValue, TRONTransferEvidenceSchema, true, CoverageIncomplete, 0},
		{"logs unaffected by excluded traces", EventKindContractLog, TRONTransferEvidenceSchema, true, CoverageComplete, 2},
		{"unknown schema native", EventKindNativeValue, 99, false, CoverageUnknown, 0},
		{"unknown schema logs", EventKindContractLog, 99, false, CoverageUnknown, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := BlockchainEvent{TxHash: "tx", ToAddress: "receiver", EventKind: tc.kind}
			events := []BlockchainEvent{event, event, {TxHash: "other", ToAddress: "receiver", EventKind: tc.kind}}
			var sources []VerificationSource
			if tc.excluded {
				sources = []VerificationSource{{ExcludedTraces: []TraceExclusion{{Note: "metadata", Count: 1}}}}
			}
			coverage := NewAMLCoverageIndex(events, sources, tc.schema).CoverageFor(event)
			require.Equal(t, tc.status, coverage.Status())
			require.Equal(t, 2, coverage.ObservedEffects())
			require.Equal(t, tc.matching, coverage.MatchingEffects())
		})
	}
}

func TestAMLCoverageDistinguishesProvenAbsenceFromUnknown(t *testing.T) {
	event := BlockchainEvent{TxHash: "tx", ToAddress: "receiver", EventKind: EventKindNativeValue}
	complete := NewAMLCoverageIndex(nil, nil, TRONTransferEvidenceSchema).CoverageFor(event)
	require.Equal(t, CoverageComplete, complete.Status())
	require.Zero(t, complete.MatchingEffects())
	var unknown TransferCoverage
	require.Equal(t, CoverageUnknown, unknown.Status())
	require.Zero(t, unknown.MatchingEffects())
}
