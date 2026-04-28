package handlers

import (
	"context"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// refKeyToEntityName maps movement data keys to metadata entity names
// for batch-resolving UUIDs → human-readable names.
var refKeyToEntityName = map[string]struct {
	EntityName string
	URLPrefix  string // URL prefix for building frontend links
}{
	"nomenclature":  {EntityName: "Nomenclature", URLPrefix: "/catalogs/nomenclatures"},
	"warehouse":    {EntityName: "Warehouse", URLPrefix: "/catalogs/warehouses"},
	"currency":     {EntityName: "Currency", URLPrefix: "/catalogs/currencies"},
	"counterparty": {EntityName: "Counterparty", URLPrefix: "/catalogs/counterparties"},
	"contract":     {EntityName: "Contract", URLPrefix: "/catalogs/contracts"},
	"organization": {EntityName: "Organization", URLPrefix: "/catalogs/organizations"},
}

// enrichMovementRefs batch-resolves all ref-type fields in movement data
// from raw UUIDs to human-readable names.
// Mutates movements in-place for efficiency (no copy).
//
// Algorithm:
// 1. Scan all movements → collect unique (entityName, id) pairs from ref-type data values
// 2. Batch-resolve via RefResolver (one query per entity type → no N+1)
// 3. Replace Name field in MovementRefValue with resolved presentation
func enrichMovementRefs(ctx context.Context, movements []entity.DocumentMovement, resolver domain.RefResolver) {
	if len(movements) == 0 {
		return
	}

	// Collect all unique ref requests
	type refKey struct {
		entityName string
		id         string
	}
	seen := make(map[refKey]bool)
	var requests = make([]domain.RefResolveRequest, 0, len(refKeyToEntityName))

	for _, m := range movements {
		for _, col := range m.Columns {
			if col.Type != "ref" {
				continue
			}
			refVal, ok := m.Data[col.Key].(entity.MovementRefValue)
			if !ok {
				continue
			}
			mapping, mapped := refKeyToEntityName[col.Key]
			if !mapped {
				continue
			}

			key := refKey{entityName: mapping.EntityName, id: refVal.ID}
			if seen[key] {
				continue
			}
			seen[key] = true

			parsedID, err := id.Parse(refVal.ID)
			if err != nil {
				continue
			}
			requests = append(requests, domain.RefResolveRequest{
				RefType: mapping.EntityName,
				RefID:   parsedID,
			})
		}
	}

	if len(requests) == 0 {
		return
	}

	// Batch resolve
	results, err := resolver.ResolveRefs(ctx, requests)
	if err != nil {
		return // Non-fatal — fall back to raw IDs
	}

	// Build lookup: (entityName, id) → resolved name
	resolved := make(map[refKey]string)
	for _, r := range results {
		if r.Presentation != "" {
			resolved[refKey{entityName: r.RefType, id: r.RefID.String()}] = r.Presentation
		}
	}

	// Enrich movement data in-place
	for i, m := range movements {
		for _, col := range m.Columns {
			if col.Type != "ref" {
				continue
			}
			refVal, ok := m.Data[col.Key].(entity.MovementRefValue)
			if !ok {
				continue
			}
			mapping, mapped := refKeyToEntityName[col.Key]
			if !mapped {
				continue
			}

			key := refKey{entityName: mapping.EntityName, id: refVal.ID}
			if name, found := resolved[key]; found {
				refVal.Name = name
				refVal.URL = mapping.URLPrefix + "/" + refVal.ID
				movements[i].Data[col.Key] = refVal
			}
		}
	}
}
