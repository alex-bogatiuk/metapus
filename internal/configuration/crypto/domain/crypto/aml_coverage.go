package crypto

import "metapus/internal/core/id"

// CoverageStatus distinguishes absence of a matching effect from absence of
// proof that the decoder observed every effect in the agreed transaction.
type CoverageStatus uint8

const (
	CoverageUnknown CoverageStatus = iota
	CoverageIncomplete
	CoverageComplete
)

// TransferCoverage can only be produced by the complete-block coverage index.
// ObservedEffects remains useful for diagnostics when coverage is incomplete;
// MatchingEffects exposes a count only when that count is proven exhaustive.
type TransferCoverage struct {
	status          CoverageStatus
	observedEffects int
}

func (c TransferCoverage) Status() CoverageStatus { return c.status }
func (c TransferCoverage) ObservedEffects() int   { return c.observedEffects }
func (c TransferCoverage) MatchingEffects() int {
	if c.status != CoverageComplete {
		return 0
	}
	return c.observedEffects
}

// AMLCoverageIndex consumes all events from one complete agreed block, before
// wallet/asset filtering. Evidence schemas describe the strength of that proof;
// they do not select a decoding implementation.
type AMLCoverageIndex struct {
	counts         map[amlCoverageKey]int
	nativeCoverage CoverageStatus
	logCoverage    CoverageStatus
}
type amlCoverageKey struct {
	Network                 id.ID
	Tx, Recipient, Contract string
}

func NewAMLCoverageIndex(events []BlockchainEvent, sources []VerificationSource, schema int) AMLCoverageIndex {
	index := AMLCoverageIndex{counts: make(map[amlCoverageKey]int)}
	if ValidTransferEvidenceSchema(schema) {
		index.logCoverage = CoverageComplete
	}
	if schema == TRONTransferEvidenceSchema {
		index.nativeCoverage = CoverageComplete
		for _, source := range sources {
			if len(source.ExcludedTraces) > 0 {
				index.nativeCoverage = CoverageIncomplete
				break
			}
		}
	}
	for _, event := range events {
		index.counts[amlCoverageKey{event.NetworkID, event.TxHash, event.ToAddress, event.TokenContract}]++
	}
	return index
}

func (index AMLCoverageIndex) CoverageFor(expected BlockchainEvent) TransferCoverage {
	status := index.nativeCoverage
	if expected.EffectiveEventKind() == EventKindContractLog {
		status = index.logCoverage
	}
	return TransferCoverage{status: status, observedEffects: index.counts[amlCoverageKey{expected.NetworkID, expected.TxHash, expected.ToAddress, expected.TokenContract}]}
}
