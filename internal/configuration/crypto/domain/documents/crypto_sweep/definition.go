package crypto_sweep

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "crypto_sweep",
	Name:  DocumentType,
	Route: "crypto-sweep",
	Table: "doc_crypto_sweeps",
	Kind:  entity.DocumentKind,
	Parts: []entity.Part{{Key: LinesPart, Field: "lines"}},
	// Platform costs are assembled during posting and have no table part.
	DerivedParts: []string{"platformCostLines"},
	Number:       entity.NumberRule{Prefix: "CS"},
}

// LinesPart is the SQL key of the per-wallet sweep lines.
const LinesPart = "lines"

func (CryptoSweep) EntityDefinition() entity.Definition { return Definition }
