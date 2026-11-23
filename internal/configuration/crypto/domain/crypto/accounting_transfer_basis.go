package crypto

import (
	"context"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// AccountingTransferBasis freezes a physical transfer independently of an
// invoice or merchant payment. The controlled side and asset are resolved from
// the managed wallet/token. An expense additionally pins its external recipient.
type AccountingTransferBasis struct {
	NetworkID, WalletID, TokenID id.ID
	TxHash                       string
	EventKind                    EventKind
	EventIndex                   int64
	FromAddress                  string
	ToAddress                    string
	RecordType                   entity.RecordType
	Amount                       types.RawAmount
	BlockNumber                  int64
	BlockHash                    string
	RequiredConfs                int
	VerificationID               *id.ID
}

func (b AccountingTransferBasis) Validate() error {
	if id.IsNil(b.NetworkID) || id.IsNil(b.WalletID) || id.IsNil(b.TokenID) ||
		!b.EventKind.Valid() || b.EventIndex < 0 ||
		(b.RecordType != "" && b.RecordType != entity.RecordTypeReceipt && b.RecordType != entity.RecordTypeExpense) ||
		(b.RecordType == entity.RecordTypeExpense && b.ToAddress == "") ||
		(b.RecordType != entity.RecordTypeExpense && b.FromAddress == "") ||
		!b.Amount.IsPositive() || b.BlockNumber < 0 || b.RequiredConfs < 1 ||
		!validAccountingHash(canonicalAccountingHash(b.TxHash)) ||
		!validAccountingHash(canonicalAccountingHash(b.BlockHash)) ||
		b.VerificationID == nil || id.IsNil(*b.VerificationID) {
		return apperror.NewValidation("verified transfer accounting basis is required")
	}
	return nil
}

// VerifiedCustodyEffect is historical verified evidence, not a spending permit
// or a financial recognition. Admission additionally checks the current
// canonical generation, posting owner and available allocation capacity.
type VerifiedCustodyEffect struct {
	VerificationID id.ID
	OccurrenceID   id.ID
	Occurrence     AccountingEffectOccurrence
}

type CustodyEffectGuard interface {
	AuthorizeCustodyEffect(context.Context, AccountingTransferBasis, BlockchainEvent) (VerifiedCustodyEffect, error)
}
