package crypto

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"

	"github.com/stretchr/testify/require"
)

func TestAccountingTransferBasisRequiresPhysicalEvidenceWithoutMerchant(t *testing.T) {
	event := accountingEvent()
	proofID := id.New()
	basis := AccountingTransferBasis{
		NetworkID: event.NetworkID, WalletID: id.New(), TokenID: id.New(),
		TxHash: event.TxHash, EventKind: event.EventKind, EventIndex: event.EventIndex,
		FromAddress: event.FromAddress, Amount: event.Amount, BlockNumber: event.BlockNumber,
		BlockHash: event.BlockHash, RequiredConfs: 19, VerificationID: &proofID,
	}
	require.NoError(t, basis.Validate())
	for _, edit := range []func(*AccountingTransferBasis){
		func(b *AccountingTransferBasis) { b.NetworkID = id.Nil() },
		func(b *AccountingTransferBasis) { b.WalletID = id.Nil() },
		func(b *AccountingTransferBasis) { b.TokenID = id.Nil() },
		func(b *AccountingTransferBasis) { b.VerificationID = nil },
		func(b *AccountingTransferBasis) { zero := id.Nil(); b.VerificationID = &zero },
		func(b *AccountingTransferBasis) { b.Amount = types.ZeroRawAmount() },
		func(b *AccountingTransferBasis) { b.EventKind = "" },
		func(b *AccountingTransferBasis) { b.EventIndex = -1 },
		func(b *AccountingTransferBasis) { b.RequiredConfs = 0 },
		func(b *AccountingTransferBasis) { b.TxHash = "unverified" },
		func(b *AccountingTransferBasis) { b.BlockHash = "" },
	} {
		changed := basis
		edit(&changed)
		require.Error(t, changed.Validate())
	}
}

func TestOutgoingAccountingBasisPinsExternalRecipient(t *testing.T) {
	event := accountingEvent()
	proof := id.New()
	b := AccountingTransferBasis{NetworkID: event.NetworkID, WalletID: id.New(), TokenID: id.New(), TxHash: event.TxHash, EventKind: event.EventKind, EventIndex: event.EventIndex,
		RecordType: entity.RecordTypeExpense, ToAddress: event.ToAddress, Amount: event.Amount, BlockNumber: event.BlockNumber, BlockHash: event.BlockHash, RequiredConfs: 1, VerificationID: &proof}
	require.NoError(t, b.Validate())
	b.ToAddress = ""
	require.Error(t, b.Validate())
	b.ToAddress = event.ToAddress
	b.RecordType = "other"
	require.Error(t, b.Validate())
}
