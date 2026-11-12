package crypto_platform_funding

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestPrepareTakesTheWholeRemainderWithItsReason(t *testing.T) {
	receipt := id.New()
	f, err := Prepare(receipt, id.New(), id.New(), types.MustCryptoAmount("1000"), "  own treasury transfer  ", nil, false, id.New(), time.Now())
	require.NoError(t, err)
	require.Equal(t, StatusPrepared, f.Status)
	require.Equal(t, "own treasury transfer", f.Reason)
	require.Equal(t, receipt, *f.BasisID)
	require.Nil(t, f.MerchantID)

	movements, err := f.GenerateCryptoReceiptSuspenseMovements(context.Background())
	require.NoError(t, err)
	require.Len(t, movements, 1, "only suspense moves: custody already holds the transfer")
	require.Equal(t, "1000", movements[0].Amount.String())
	require.Equal(t, f.SuspenseLineID, movements[0].LineID)

	_, err = Prepare(receipt, id.New(), id.New(), types.MustCryptoAmount("1"), " ", nil, false, id.New(), time.Now())
	require.Error(t, err, "a reason is required")
}

func TestMerchantTiedReceiptNeedsAcknowledgement(t *testing.T) {
	tie := MerchantTie{MerchantID: id.New(), MerchantName: "CryptoGate", Link: LinkCustodyOwner}
	_, err := Prepare(id.New(), id.New(), id.New(), types.MustCryptoAmount("5"), "funding", &tie, false, id.New(), time.Now())
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, apperror.CodeValidation, appErr.Code)
	require.Equal(t, "CryptoGate", appErr.Details["merchantName"])

	f, err := Prepare(id.New(), id.New(), id.New(), types.MustCryptoAmount("5"), "funding", &tie, true, id.New(), time.Now())
	require.NoError(t, err)
	require.True(t, f.MerchantLinkAcknowledged)
	require.True(t, f.TieMatches(&tie))
	require.False(t, f.TieMatches(nil), "a tie that disappeared is a change")
	require.False(t, f.TieMatches(&MerchantTie{MerchantID: id.New(), Link: LinkCustodyOwner}))
}

func TestApprovalAlwaysRequiresAnotherOperator(t *testing.T) {
	actor := id.New()
	f, err := Prepare(id.New(), id.New(), id.New(), types.MustCryptoAmount("10"), "funding", nil, false, actor, time.Now())
	require.NoError(t, err)
	require.Error(t, f.Approve(actor, time.Now()), "the preparing operator never approves platform funding")
	require.NoError(t, f.Approve(id.New(), time.Now()))
	require.Equal(t, StatusPosted, f.Status)
	require.Error(t, f.Reject(id.New(), "late", time.Now()), "a decided funding cannot change")

	rejected, err := Prepare(id.New(), id.New(), id.New(), types.MustCryptoAmount("10"), "funding", nil, false, actor, time.Now())
	require.NoError(t, err)
	require.Error(t, rejected.Reject(actor, " ", time.Now()), "a rejection needs a reason")
	require.NoError(t, rejected.Reject(actor, "it was a customer payment", time.Now()))
	require.Equal(t, StatusRejected, rejected.Status)
}
