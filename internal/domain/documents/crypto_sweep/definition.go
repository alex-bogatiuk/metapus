package crypto_sweep

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "crypto_sweep",
	Name:  DocumentType,
	Route: "crypto-sweep",
	Table: "doc_crypto_sweeps",
	Kind:  entity.DocumentKind,
}

func (CryptoSweep) EntityDefinition() entity.Definition { return Definition }
