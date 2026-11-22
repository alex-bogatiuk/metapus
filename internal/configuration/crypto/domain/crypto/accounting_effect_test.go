package crypto

import (
	"strings"
	"testing"
	"time"

	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/types"

	"github.com/stretchr/testify/require"
)

func accountingEvent() BlockchainEvent {
	return BlockchainEvent{
		NetworkID: id.MustParse("01900000-0000-7000-8000-000000000001"),
		TxHash:    "0x" + strings.Repeat("a", 64), EventKind: EventKindContractLog, EventIndex: 7,
		FromAddress: "0xABC", ToAddress: "0xDEF", TokenContract: "0x123",
		Amount: types.MustRawAmount("12000000"), BlockNumber: 100,
		BlockHash: "0x" + strings.Repeat("b", 64), Confirmations: 20,
		Finality: FinalitySolidified, Canonicality: CanonicalityCanonical,
	}
}

func TestAccountingEffectIdentityGoldenAndReinclusion(t *testing.T) {
	first := accountingEvent()
	legacyHash := first.EconomicsHash()
	identity, err := NewAccountingEffectIdentity(first, ChainFamilyEVM, 2)
	require.NoError(t, err)
	key, err := identity.Key()
	require.NoError(t, err)
	require.Equal(t, "accounting-effect.v1:8625c5bf3cc6ff04e14827b891f5b53d38925a04575f31e3051cb2e8b7e321bd", key)
	before, err := identity.Occurrence(first)
	require.NoError(t, err)
	// Same transaction included after a different set of earlier transactions.
	// Its block-global log index and block hash legitimately change.
	second := first
	second.EventIndex, second.BlockNumber = 91, 103
	second.BlockHash = "0x" + strings.Repeat("c", 64)
	second.FromAddress, second.ToAddress = strings.ToLower(first.FromAddress), strings.ToLower(first.ToAddress)
	second.TxHash = strings.ToUpper(first.TxHash)
	reincluded, err := NewAccountingEffectIdentity(second, ChainFamilyEVM, 2)
	require.NoError(t, err)
	require.Equal(t, identity, reincluded)
	after, err := reincluded.Occurrence(second)
	require.NoError(t, err)
	require.Equal(t, before.EconomicsHash, after.EconomicsHash)
	require.NotEqual(t, before.BlockHash, after.BlockHash)
	require.Equal(t, legacyHash, first.EconomicsHash(), "new identity cannot mutate frozen transfer economics")
}

func TestAccountingEffectSeparatesIdentityFromEconomics(t *testing.T) {
	event := accountingEvent()
	identity, err := NewAccountingEffectIdentity(event, ChainFamilyEVM, 2)
	require.NoError(t, err)
	original, err := identity.Occurrence(event)
	require.NoError(t, err)
	for _, change := range []func(*BlockchainEvent){
		func(e *BlockchainEvent) { e.Amount = e.Amount.Add(types.MustRawAmount("1")) },
		func(e *BlockchainEvent) { e.FromAddress = "different-source" },
		func(e *BlockchainEvent) { e.ToAddress = "different-destination" },
		func(e *BlockchainEvent) { e.TokenContract = "different-contract" },
	} {
		changed := event
		change(&changed)
		occurrence, err := identity.Occurrence(changed)
		require.NoError(t, err)
		require.NotEqual(t, original.EconomicsHash, occurrence.EconomicsHash)
		require.Equal(t, original.Identity, occurrence.Identity, "conflicting economics cannot acquire a new logical identity")
	}
	for _, change := range []func(*AccountingEffectIdentity){
		func(a *AccountingEffectIdentity) { a.NetworkID = id.New() },
		func(a *AccountingEffectIdentity) { a.Ordinal++ },
		func(a *AccountingEffectIdentity) { a.TxHash = strings.Repeat("d", 64) },
	} {
		other := identity
		change(&other)
		key, err := other.Key()
		require.NoError(t, err)
		originalKey, err := identity.Key()
		require.NoError(t, err)
		require.NotEqual(t, originalKey, key)
	}
}

func TestAccountingTRONInternalIdentityBindsFullTrace(t *testing.T) {
	event := accountingEvent()
	event.DecoderVersion, event.EventKind, event.TokenContract = TRONTransferEvidenceSchema, EventKindInternalValue, ""
	callIndex, rawIndex := int64(2), int64(99)
	event.TraceHash, event.CallValueIndex, event.TraceIndex = strings.Repeat("e", 64), &callIndex, &rawIndex
	identity, err := NewAccountingEffectIdentity(event, ChainFamilyTRON, callIndex)
	require.NoError(t, err)
	before, err := identity.Occurrence(event)
	require.NoError(t, err)
	event.EventIndex, rawIndex = 900, 5
	after, err := identity.Occurrence(event)
	require.NoError(t, err)
	require.Equal(t, before.EconomicsHash, after.EconomicsHash)
	event.TraceHash = strings.Repeat("f", 64)
	require.Error(t, identity.ValidateEvent(event))
	other, err := NewAccountingEffectIdentity(event, ChainFamilyTRON, callIndex)
	require.NoError(t, err)
	firstKey, err := identity.Key()
	require.NoError(t, err)
	secondKey, err := other.Key()
	require.NoError(t, err)
	require.NotEqual(t, firstKey, secondKey)
}

func TestAccountingEffectRejectsUnprovenAndMalformedIdentity(t *testing.T) {
	for _, test := range []struct {
		name    string
		edit    func(*BlockchainEvent)
		family  ChainFamily
		ordinal int64
	}{
		{"negative ordinal", func(*BlockchainEvent) {}, ChainFamilyEVM, -1},
		{"unknown family", func(*BlockchainEvent) {}, ChainFamily("unknown"), 2},
		{"nil network", func(e *BlockchainEvent) { e.NetworkID = id.Nil() }, ChainFamilyEVM, 2},
		{"malformed hash", func(e *BlockchainEvent) { e.TxHash = "tx" }, ChainFamilyEVM, 2},
		{"unsupported kind", func(e *BlockchainEvent) { e.EventKind = "" }, ChainFamilyEVM, 2},
		{"ordinal above block index", func(e *BlockchainEvent) { e.EventIndex = 1 }, ChainFamilyEVM, 2},
		{"native nonzero ordinal", func(e *BlockchainEvent) { e.EventKind = EventKindNativeValue }, ChainFamilyEVM, 2},
		{"EVM internal without traces", func(e *BlockchainEvent) { e.EventKind = EventKindInternalValue }, ChainFamilyEVM, 2},
		{"TRON legacy evidence", func(*BlockchainEvent) {}, ChainFamilyTRON, 7},
		{"TRON ordinal mismatch", func(e *BlockchainEvent) { e.DecoderVersion = 2 }, ChainFamilyTRON, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := accountingEvent()
			test.edit(&event)
			_, err := NewAccountingEffectIdentity(event, test.family, test.ordinal)
			require.Error(t, err)
		})
	}
}

func TestAccountingEvidenceIsOptionalAndCannotRebindProof(t *testing.T) {
	event := accountingEvent()
	event.TxHash, event.BlockHash = event.TxHash[2:], event.BlockHash[2:]
	event.DecoderVersion, event.EventIndex = 2, 2
	proof := TransferVerification{
		Event: event, DecoderVersion: 2, VerifiedAt: time.Unix(1750000000, 0).UTC(),
		Outcome: "verified", ExecutionStatus: ReceiptSucceeded,
		Sources: []VerificationSource{
			{ProviderID: "one", LatestProviderID: "one", TrustGroup: "one", Digest: "block", SourceDigest: "source", HeadHeight: 119, HeadHash: "head", DecoderVersion: 2},
			{ProviderID: "two", LatestProviderID: "two", TrustGroup: "two", Digest: "block", SourceDigest: "source", HeadHeight: 119, HeadHash: "head", DecoderVersion: 2},
		},
	}
	require.NoError(t, proof.Validate())
	oldRaw, err := json.Marshal(proof)
	require.NoError(t, err)
	require.NotContains(t, string(oldRaw), "accountingIdentity")
	economics := proof.Event.EconomicsHash()
	require.NoError(t, proof.BindTRONAccountingIdentity())
	require.NoError(t, proof.Validate())
	require.Equal(t, economics, proof.Event.EconomicsHash())
	proof.AccountingIdentity.NetworkID = id.New()
	require.Error(t, proof.Validate())
	proof.AccountingIdentity = nil
	proof.Event.Canonicality, proof.Event.EventType, proof.Outcome = CanonicalityOrphaned, EventTypeReorg, "absent"
	require.NoError(t, proof.Validate())
	require.Error(t, proof.BindTRONAccountingIdentity(), "absence is not another successful admission")
}
