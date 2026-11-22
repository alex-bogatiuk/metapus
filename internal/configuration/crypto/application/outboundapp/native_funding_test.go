package outboundapp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

type fundingRecoveryStore struct {
	NativeFundingRepository
	funding crypto.NativeFunding
}

func (s fundingRecoveryStore) Lookup(context.Context, string, id.ID, int) (crypto.NativeFunding, bool, error) {
	return s.funding, true, nil
}

type fundingFinalityReader struct {
	crypto.FinalityReader
	receipt crypto.ChainReceipt
}

func (s fundingFinalityReader) Receipt(context.Context, string) (crypto.ChainReceipt, error) {
	return s.receipt, nil
}

type fundingRecoveryDriver struct {
	crypto.NetworkDriver
	network crypto.NetworkRef
	receipt crypto.ChainReceipt
}

func (s fundingRecoveryDriver) Network() crypto.NetworkRef { return s.network }
func (s fundingRecoveryDriver) Finality() crypto.FinalityReader {
	return fundingFinalityReader{receipt: s.receipt}
}

type fundingSettlementStub struct {
	called  int
	funding id.ID
	err     error
}

func (s *fundingSettlementStub) Settle(_ context.Context, funding id.ID, _ crypto.ChainReceipt) (bool, error) {
	s.called++
	s.funding = funding
	return s.err == nil, s.err
}

type unusedFundingFees struct{ FeeBudget }
type unusedFundingAuthorization struct{ FundingAuthorizations }

type fundingEvidenceStub struct{ err error }

func (s fundingEvidenceStub) VerifyFunding(context.Context, crypto.NetworkDriver, crypto.NativeFunding, crypto.ChainReceipt) error {
	return s.err
}

func TestFundingRecoveryUsesSavedEffectWhenCurrentJITPolicyIsDisabled(t *testing.T) {
	network, wallet, fundingID := id.New(), id.New(), id.New()
	funding := crypto.NativeFunding{ID: fundingID, NetworkID: network, DestinationWalletID: wallet, Status: "broadcast", TxHash: "saved-funding"}
	driver := fundingRecoveryDriver{network: crypto.NetworkRef{ID: network}, receipt: crypto.ChainReceipt{NetworkID: network, TxHash: funding.TxHash, Status: crypto.ReceiptSucceeded, Finality: crypto.FinalityFinalized, Canonicality: crypto.CanonicalityCanonical}}
	settlement := &fundingSettlementStub{}
	service := NewNativeFundingService(fundingRecoveryStore{funding: funding}, nil, unusedFundingFees{}, unusedFundingAuthorization{}, settlement, nil, fundingEvidenceStub{})
	request := NativeFundingRequest{BasisType: "CryptoSweepAttempt", BasisID: id.New(), NetworkID: network, WalletID: wallet, Address: "destination"}
	ready, err := service.Ensure(t.Context(), driver, crypto.NetworkPolicy{}, request, nil, types.NewRawAmountFromInt64(1))
	require.NoError(t, err)
	require.True(t, ready)
	require.Equal(t, fundingID, settlement.funding)
	// No source lookup, new reservation, policy signing or broadcast is configured.
	// A temporary settlement failure must remain retryable, never a permanent hold.
	temporary := errors.New("database unavailable during atomic settlement")
	settlement.err = temporary
	ready, err = service.Ensure(t.Context(), driver, crypto.NetworkPolicy{}, request, nil, types.NewRawAmountFromInt64(1))
	require.ErrorIs(t, err, temporary)
	require.False(t, ready)
	require.Equal(t, 2, settlement.called)
	service.evidence = fundingEvidenceStub{err: crypto.ErrVerificationPending}
	_, err = service.Ensure(t.Context(), driver, crypto.NetworkPolicy{}, request, nil, types.NewRawAmountFromInt64(1))
	require.ErrorIs(t, err, crypto.ErrVerificationPending)
	require.ErrorIs(t, err, ErrNativeFundingPending, "proof delay must keep the parent waiting rather than permanently held")
	require.Equal(t, 2, settlement.called, "missing principal evidence must not enter fee/principal settlement")
}

type fundingTopUpStore struct {
	NativeFundingRepository
	prior   crypto.NativeFunding
	created []crypto.NativeFunding
}

var errStopAfterCreate = errors.New("stop after funding intent")

func (s *fundingTopUpStore) Lookup(context.Context, string, id.ID, int) (crypto.NativeFunding, bool, error) {
	return s.prior, true, nil
}
func (s *fundingTopUpStore) OperatingBalance(context.Context, id.ID, id.ID) (types.RawAmount, bool, error) {
	return types.ZeroRawAmount(), false, nil // no AML operating partition
}
func (s *fundingTopUpStore) LoadWallet(_ context.Context, wallet, _ id.ID, _ string) (HotWallet, error) {
	return HotWallet{ID: wallet, Address: "destination"}, nil
}
func (s *fundingTopUpStore) Create(_ context.Context, funding crypto.NativeFunding) (crypto.NativeFunding, error) {
	s.created = append(s.created, funding)
	return funding, errStopAfterCreate
}

type fundingSource struct{ wallet HotWallet }

func (s fundingSource) ResolveNativeFundingSource(context.Context, id.ID, id.ID) (HotWallet, error) {
	return s.wallet, nil
}

type fundingBalances struct {
	crypto.BalanceReader
	balance types.RawAmount
}

func (s fundingBalances) NativeBalance(context.Context, string) (types.RawAmount, error) {
	return s.balance, nil
}

type fundingTopUpDriver struct {
	crypto.NetworkDriver
	network crypto.NetworkRef
	balance types.RawAmount
}

func (s fundingTopUpDriver) Network() crypto.NetworkRef { return s.network }
func (s fundingTopUpDriver) Balances() crypto.BalanceReader {
	return fundingBalances{balance: s.balance}
}

// Stand CS-2026-00005: after round 1 settled, the re-quoted sweep fee grew and
// the sweep was held forever, because a wallet without an AML operating
// partition was reported ready without looking at its balance.
func TestSettledFundingTopsUpWhenTheQuoteOutgrewTheBalance(t *testing.T) {
	network, wallet := id.New(), id.New()
	policy := crypto.NetworkPolicy{FeeFunding: crypto.FeeFundingPolicy{Mode: crypto.FeeFundingJIT,
		MinSourceWalletResidual: types.MustRawAmount("100000000000000"), MaxFundingAmount: types.MustRawAmount("1000000000000000")}}
	request := NativeFundingRequest{BasisType: "CryptoSweepAttempt", BasisID: id.New(), NetworkID: network, WalletID: wallet, Address: "destination"}
	requiredFee := types.MustRawAmount("189653003224101")
	prior := crypto.NativeFunding{ID: id.New(), NetworkID: network, DestinationWalletID: wallet, Status: "settled", Attempt: 1}

	store := &fundingTopUpStore{prior: prior}
	driver := fundingTopUpDriver{network: crypto.NetworkRef{ID: network}, balance: types.MustRawAmount("289653003034448")}
	service := NewNativeFundingService(store, fundingSource{wallet: HotWallet{ID: id.New(), Address: "fee"}}, unusedFundingFees{}, unusedFundingAuthorization{}, &fundingSettlementStub{}, nil, fundingEvidenceStub{})
	ready, err := service.Ensure(t.Context(), driver, policy, request, nil, requiredFee)
	require.ErrorIs(t, err, errStopAfterCreate)
	require.False(t, ready)
	require.Len(t, store.created, 1)
	require.Equal(t, 2, store.created[0].Attempt, "the next bounded top-up round")
	require.Equal(t, "189653", store.created[0].Amount.String(), "exactly the deficit")

	store = &fundingTopUpStore{prior: prior}
	driver.balance = types.MustRawAmount("300000000000000")
	service = NewNativeFundingService(store, nil, unusedFundingFees{}, unusedFundingAuthorization{}, &fundingSettlementStub{}, nil, fundingEvidenceStub{})
	ready, err = service.Ensure(t.Context(), driver, crypto.NetworkPolicy{}, request, nil, requiredFee)
	require.NoError(t, err)
	require.True(t, ready, "a sufficient chain balance needs no policy or new round")
	require.Empty(t, store.created)
}
