package crypto

import (
	"strings"
	"testing"
	"time"

	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"

	"github.com/stretchr/testify/require"
)

func networkFeeGolden() NetworkFeeProof {
	p := NetworkFeeProof{SchemaVersion: 1, Family: ChainFamilyEVM, NetworkID: id.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), NativeTokenID: id.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"), TxHash: "0x" + strings.Repeat("1", 64), PayerAddress: "0x00000000000000000000000000000000000000AB", BlockNumber: 100, BlockHash: "0x" + strings.Repeat("2", 64), Status: ReceiptReverted,
		TotalFee: types.MustRawAmount("42000"), Components: []FeeComponent{{Name: "execution_gas", Amount: types.MustRawAmount("42000"), Quantity: 21000}}, Confirmations: 20, Finality: FinalitySolidified, Canonicality: CanonicalityCanonical, VerifiedAt: time.Unix(1750000000, 0)}
	p.Sources = []VerificationSource{{ProviderID: "one", TrustGroup: "one", HeadHeight: 119, HeadHash: strings.Repeat("3", 64), SourceDigest: strings.Repeat("4", 64)}, {ProviderID: "two", TrustGroup: "two", HeadHeight: 119, HeadHash: strings.Repeat("3", 64), SourceDigest: strings.Repeat("5", 64)}}
	for i := range p.Sources {
		p.Sources[i].Digest = p.Digest()
	}
	return p
}
func TestNetworkFeeIdentityAndProofGolden(t *testing.T) {
	p := networkFeeGolden()
	require.NoError(t, p.Validate())
	require.Equal(t, "07a96a578c2cdae696c144ead6d0d652dc99799283e05762c25056814c46c805", p.EffectKey())
	require.Equal(t, "44d622c9fac1b5ecee2a03bc799eecab13304f1e6745ccba3b72421e7c75ac52", p.Digest())
	alias := p
	alias.TxHash = strings.ToUpper(strings.TrimPrefix(p.TxHash, "0x"))
	alias.PayerAddress = strings.ToLower(p.PayerAddress)
	require.Equal(t, p.EffectKey(), alias.EffectKey())
	require.Equal(t, p.Digest(), alias.Digest())
	changed := p
	changed.BlockHash = strings.Repeat("6", 64)
	require.Equal(t, p.EffectKey(), changed.EffectKey())
	require.NotEqual(t, p.Digest(), changed.Digest())
}
func TestNetworkFeeProofRejectsAmbiguousOrIncompleteEvidence(t *testing.T) {
	for _, change := range []func(*NetworkFeeProof){
		func(p *NetworkFeeProof) { p.Sources = p.Sources[:1] }, func(p *NetworkFeeProof) { p.Sources[1].TrustGroup = p.Sources[0].TrustGroup },
		func(p *NetworkFeeProof) { p.Sources[1].HeadHash = strings.Repeat("7", 64) }, func(p *NetworkFeeProof) { p.Sources[1].HeadHeight++ },
		func(p *NetworkFeeProof) { p.TotalFee = types.ZeroRawAmount() }, func(p *NetworkFeeProof) { p.Components = append(p.Components, p.Components[0]) },
		func(p *NetworkFeeProof) { p.Components[0].Name = "unknown_fee" }, func(p *NetworkFeeProof) { p.Canonicality = CanonicalityOrphaned }, func(p *NetworkFeeProof) { p.Status = ReceiptPending },
		func(p *NetworkFeeProof) { p.SchemaVersion = 2 }, func(p *NetworkFeeProof) { p.Components[0].Quantity = -1 },
	} {
		p := networkFeeGolden()
		change(&p)
		require.Error(t, p.Validate())
	}
	p := networkFeeGolden()
	p.TotalFee = types.ZeroRawAmount()
	p.Components[0].Amount = types.ZeroRawAmount()
	for i := range p.Sources {
		p.Sources[i].Digest = p.Digest()
	}
	require.NoError(t, p.Validate())
}
