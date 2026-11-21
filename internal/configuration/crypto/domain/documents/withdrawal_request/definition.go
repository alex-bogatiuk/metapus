package withdrawal_request

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:    "withdrawal_request",
	Name:   DocumentType,
	Route:  "withdrawal-request",
	Table:  "doc_withdrawal_requests",
	Kind:   entity.DocumentKind,
	Number: entity.NumberRule{Prefix: "WR"},
}

func (WithdrawalRequest) EntityDefinition() entity.Definition { return Definition }
