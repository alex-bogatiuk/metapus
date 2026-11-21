package treasury_withdrawal

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "treasury_withdrawal",
	Name:  DocumentType,
	Route: "treasury-withdrawal",
	Table: "doc_treasury_withdrawals",
	Kind:  entity.DocumentKind,
	// Fee sources are loaded from fee reservations and have no table part.
	DerivedParts: []string{"feeLines"},
	Number:       entity.NumberRule{Prefix: "TW"},
}

func (TreasuryWithdrawal) EntityDefinition() entity.Definition { return Definition }
