package rate_source

import "metapus/internal/core/entity"

var Definition = entity.Definition{
	Key:   "rate_source",
	Name:  "RateSource",
	Route: "rate-sources",
	Table: "cat_rate_sources",
	Kind:  entity.CatalogKind,
}

func (RateSource) EntityDefinition() entity.Definition { return Definition }
