package outboundapp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
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
