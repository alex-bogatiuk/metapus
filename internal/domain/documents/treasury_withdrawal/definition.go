package treasury_withdrawal

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "treasury_withdrawal",
	Name:  DocumentType,
	Route: "treasury-withdrawal",
	Table: "doc_treasury_withdrawals",
	Kind:  entity.DocumentKind,
}

func (TreasuryWithdrawal) EntityDefinition() entity.Definition { return Definition }
