package crypto_custody_receipt

import (
	"testing"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/registers/cryptoposting"

	"github.com/stretchr/testify/require"
)

func receiptModel() *CryptoCustodyReceipt {
	doc := New()
	doc.NetworkID, doc.WalletID, doc.TokenID = id.New(), id.New(), id.New()
	doc.PostingID, doc.OccurrenceID, doc.VerificationID = id.New(), id.New(), id.New()
	doc.Number, doc.ReasonCode = "CCR-2026-1", ReasonNoAllocation
	doc.BasisType, doc.BasisID = "CryptoTransferVerification", &doc.VerificationID
	doc.Amount = types.MustRawAmount("123456789012345678901234567890")
	return doc
}

func TestReceiptCreatesBalancedCustodyAndSuspenseWithoutAttribution(t *testing.T) {
	doc := receiptModel()
	require.NoError(t, doc.CanPost(t.Context()))
	physical, err := doc.GenerateCryptoBalanceMovements(t.Context())
	require.NoError(t, err)
	suspense, err := doc.GenerateCryptoReceiptSuspenseMovements(t.Context())
	require.NoError(t, err)
	require.Len(t, physical, 1)
	require.Len(t, suspense, 1)
	require.Equal(t, physical[0].Amount.String(), suspense[0].Amount.String())
	require.Equal(t, doc.Amount.String(), physical[0].Amount.String())
	require.Equal(t, entity.RecordTypeReceipt, physical[0].RecordType)
	require.Equal(t, doc.ID, suspense[0].ReceiptID)
	require.Equal(t, doc.CustodyLineID, physical[0].LineID)
	require.Equal(t, doc.SuspenseLineID, suspense[0].LineID)
	again, err := doc.GenerateCryptoBalanceMovements(t.Context())
	require.NoError(t, err)
	require.Equal(t, physical, again, "retry does not allocate another movement identity or clock")
	_, merchantSource := interface{}(doc).(cryptoposting.CryptoMerchantBalanceMovementSource)
	_, feeSource := interface{}(doc).(cryptoposting.CryptoFeeMovementSource)
	require.False(t, merchantSource)
	require.False(t, feeSource)
	doc.MarkPosted()
	require.Error(t, doc.CanPost(t.Context()))
	require.Error(t, doc.CanUnpost(t.Context()))
	require.Error(t, doc.CanModify())
}

func TestReceiptRequiresEvidenceAndDistinctPhysicalCounterpartLines(t *testing.T) {
	for _, edit := range []func(*CryptoCustodyReceipt){
		func(r *CryptoCustodyReceipt) { r.VerificationID = id.Nil() },
		func(r *CryptoCustodyReceipt) { r.PostingID = id.Nil() },
		func(r *CryptoCustodyReceipt) { r.OccurrenceID = id.Nil() },
		func(r *CryptoCustodyReceipt) { r.SuspenseLineID = r.CustodyLineID },
		func(r *CryptoCustodyReceipt) { r.Amount = types.ZeroRawAmount() },
		func(r *CryptoCustodyReceipt) { r.ReasonCode = Reason("automatic_income") },
		func(r *CryptoCustodyReceipt) { r.BasisID = nil },
	} {
		doc := receiptModel()
		edit(doc)
		require.Error(t, doc.CanPost(t.Context()))
	}
}
