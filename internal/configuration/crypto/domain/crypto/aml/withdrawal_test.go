package aml

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func testWithdrawal() WithdrawalRequest {
	return WithdrawalRequest{MerchantID: id.New(), NetworkID: id.New(), TokenID: id.New(), Class: Low, Amount: types.NewRawAmountFromInt64(1000)}
}

func testSource(r WithdrawalRequest, class Class, available int64) Source {
	return Source{WalletID: id.New(), MerchantID: r.MerchantID, NetworkID: r.NetworkID, TokenID: r.TokenID,
		Class: class, Revision: 1, Available: types.NewRawAmountFromInt64(available)}
}

func TestSelectedClassCannotBorrowOtherRiskOrMerchant(t *testing.T) {
	request := testWithdrawal()
	low := testSource(request, Low, 700)
	medium := testSource(request, Medium, 1000)
	otherMerchant := testSource(request, Low, 1000)
	otherMerchant.MerchantID = id.New()
	otherNetwork := testSource(request, Low, 1000)
	otherNetwork.NetworkID = id.New()
	otherToken := testSource(request, Low, 1000)
	otherToken.TokenID = id.New()
	lines, err := SelectSources(request, []Source{low, medium, otherMerchant, otherNetwork, otherToken})
	require.ErrorContains(t, err, "insufficient funds in selected risk class")
	require.Nil(t, lines)
}

func TestOneClassCanUseSeveralSourcesWithoutMutatingCandidates(t *testing.T) {
	request := testWithdrawal()
	first, second := testSource(request, Low, 700), testSource(request, Low, 500)
	candidates := []Source{second, first}
	lines, err := SelectSources(request, candidates)
	require.NoError(t, err)
	require.Len(t, lines, 2)
	require.Equal(t, request.Amount.String(), lines[0].Amount.Add(lines[1].Amount).String())
	require.Equal(t, second.WalletID, candidates[0].WalletID)
	for _, line := range lines {
		require.Equal(t, Low, line.Class)
		require.Equal(t, int64(1), line.Revision)
	}
}

func TestDuplicateSourceCannotInflateAvailableAmount(t *testing.T) {
	request := testWithdrawal()
	source := testSource(request, Low, 700)
	_, err := SelectSources(request, []Source{source, source})
	require.ErrorContains(t, err, "duplicate withdrawal source")
}

func TestHighAndUnknownRequireExplicitAcceptance(t *testing.T) {
	for _, class := range []Class{High, Unknown} {
		request := testWithdrawal()
		request.Class = class
		source := testSource(request, class, 1000)
		_, err := SelectSources(request, []Source{source})
		require.ErrorContains(t, err, "explicit acceptance")
		request.RiskAccepted = true
		lines, err := SelectSources(request, []Source{source})
		require.NoError(t, err)
		require.Equal(t, class, lines[0].Class)
	}
}

func TestAutomaticConsolidationRequiresSameOwnerAssetAndClass(t *testing.T) {
	request := testWithdrawal()
	source, destination := testSource(request, Low, 100), testSource(request, Low, 0)
	require.True(t, CanConsolidate(source, destination))
	destination.MerchantID = id.New()
	require.False(t, CanConsolidate(source, destination))
	destination = testSource(request, Medium, 0)
	require.False(t, CanConsolidate(source, destination))
	source.Class, destination.Class = High, High
	require.False(t, CanConsolidate(source, destination))
}

func TestBalanceIncludesDebitFirstCommitmentExactlyOnce(t *testing.T) {
	scope := BalanceScope{MerchantID: id.New(), NetworkID: id.New(), TokenID: id.New()}
	buckets, err := SummarizeBalance(scope, []BalancePosition{{Scope: scope, Class: Low, Free: types.NewRawAmountFromInt64(390), Reserved: types.NewRawAmountFromInt64(600)}})
	require.NoError(t, err)
	require.Equal(t, "990", buckets[0].Total.String())
	require.Equal(t, "390", buckets[0].Free.String())
	require.Equal(t, "600", buckets[0].Reserved.String())
	require.Equal(t, "0", buckets[0].TechnicalHold.String())
	reclassified, err := SummarizeBalance(scope, []BalancePosition{{Scope: scope, Class: High, Free: types.NewRawAmountFromInt64(390), Reserved: types.NewRawAmountFromInt64(600)}})
	require.NoError(t, err)
	require.Equal(t, "0", reclassified[0].Total.String())
	require.Equal(t, "990", reclassified[2].Total.String())
}

func TestRiskBalanceCannotMixAssetsOrMerchants(t *testing.T) {
	scope := BalanceScope{MerchantID: id.New(), NetworkID: id.New(), TokenID: id.New()}
	for _, other := range []BalanceScope{
		{MerchantID: id.New(), NetworkID: scope.NetworkID, TokenID: scope.TokenID},
		{MerchantID: scope.MerchantID, NetworkID: id.New(), TokenID: scope.TokenID},
		{MerchantID: scope.MerchantID, NetworkID: scope.NetworkID, TokenID: id.New()},
	} {
		_, err := SummarizeBalance(scope, []BalancePosition{{Scope: other, Class: Low, Free: types.NewRawAmountFromInt64(100)}})
		require.ErrorContains(t, err, "cannot combine")
	}
}
