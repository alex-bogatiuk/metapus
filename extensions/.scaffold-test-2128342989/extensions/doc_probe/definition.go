package doc_probe

import "metapus/internal/core/entity"

// Definition is shared by persistence, service, registration and wire contracts.
var Definition = entity.Definition{
	Key: "doc_probe", Name: "DocProbe", Route: "doc-probe", Table: "doc_doc_probes",
	Kind: entity.DocumentKind,
}
