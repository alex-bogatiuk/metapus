package funding

import (
	"strings"
	"testing"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
	"github.com/stretchr/testify/require"
)

func TestFundingRequiresExactIndependentTransferProof(t *testing.T) {
	target := walletFundingTarget{networkID: id.New(), address: "TRecipient", requiredConfirmations: 7}
	txHash := strings.Repeat("a", 64)
	makeProof := func() crypto.TransferVerification {
		event := crypto.BlockchainEvent{NetworkID: target.networkID, TxHash: txHash, EventKind: crypto.EventKindNativeValue, EventIndex: 0, FromAddress: "TSender", ToAddress: target.address, Amount: types.NewRawAmountFromInt64(25_000_000), BlockNumber: 100, BlockHash: strings.Repeat("b", 64), Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical, Confirmations: 21, DecoderVersion: 2}
		sources := []crypto.VerificationSource{{ProviderID: "trongrid", LatestProviderID: "trongrid", TrustGroup: "trongrid", Digest: "same", SourceDigest: "source-a", HeadHash: "head", HeadHeight: 120, DecoderVersion: 2}, {ProviderID: "chainstack", LatestProviderID: "chainstack", TrustGroup: "chainstack", Digest: "same", SourceDigest: "source-b", HeadHash: "head", HeadHeight: 120, DecoderVersion: 2}}
		identity, err := crypto.NewAccountingEffectIdentity(event, crypto.ChainFamilyTRON, 0)
		require.NoError(t, err)
		return crypto.TransferVerification{AccountingIdentity: &identity, Event: event, Sources: sources, DecoderVersion: 2, VerifiedAt: time.Now(), Outcome: "verified", ExecutionStatus: crypto.ReceiptSucceeded}
	}
	require.NoError(t, validateFundingProof(target, txHash, makeProof()))
	for _, tt := range []struct {
		name   string
		change func(*crypto.TransferVerification)
	}{
		{"wrong recipient", func(p *crypto.TransferVerification) { p.Event.ToAddress = "TOther" }},
		{"wrong network", func(p *crypto.TransferVerification) { p.Event.NetworkID = id.New() }},
		{"wrong transaction", func(p *crypto.TransferVerification) { p.Event.TxHash = strings.Repeat("c", 64) }},
		{"wrong asset", func(p *crypto.TransferVerification) { p.Event.TokenContract = "TContract" }},
		{"one provider", func(p *crypto.TransferVerification) { p.Sources = p.Sources[:1] }},
		{"same operator", func(p *crypto.TransferVerification) { p.Sources[1].TrustGroup = p.Sources[0].TrustGroup }},
		{"different projection", func(p *crypto.TransferVerification) { p.Sources[1].Digest = "different" }},
		{"failed execution", func(p *crypto.TransferVerification) { p.ExecutionStatus = crypto.ReceiptReverted }},
		{"unindexed legacy evidence", func(p *crypto.TransferVerification) { p.AccountingIdentity = nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			proof := makeProof()
			tt.change(&proof)
			require.Error(t, validateFundingProof(target, txHash, proof))
		})
	}
	target.requiredConfirmations = 22
	require.ErrorIs(t, validateFundingProof(target, txHash, makeProof()), crypto.ErrVerificationPending)
}
