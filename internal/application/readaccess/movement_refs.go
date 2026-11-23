package readaccess

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain"
)

// movementRefURL builds the frontend link of a resolved reference.
func movementRefURL(ref domain.RefResolveResult, id string) string {
	if ref.Route == "" {
		return ""
	}
	section := "/catalogs/"
	if ref.EntityType == "document" {
		section = "/documents/"
	}
	return section + ref.Route + "/" + id
}

// EnrichMovementRefs batch-resolves all ref-type fields in movement data
// from raw UUIDs to human-readable names.
// Mutates movements in-place for efficiency (no copy).
//
// Algorithm:
// 1. Scan all movements → collect unique (entityName, id) pairs from ref-type data values
// 2. Batch-resolve via RefResolver (one query per entity type → no N+1)
// 3. Replace Name field in MovementRefValue with resolved presentation
func EnrichMovementRefs(ctx context.Context, movements []entity.DocumentMovement, resolver domain.RefResolver) error {
	if len(movements) == 0 {
		return nil
	}

	// Collect all unique ref requests
	type refKey struct {
		entityName string
		id         string
	}
	seen := make(map[refKey]bool)
	var requests []domain.RefResolveRequest

	for _, m := range movements {
		for _, col := range m.Columns {
			if col.Type != "ref" {
				continue
			}
			refVal, ok := m.Data[col.Key].(entity.MovementRefValue)
			if !ok {
				delete(m.Data, col.Key)
				continue
			}
			if col.RefEntity == "" {
				delete(m.Data, col.Key)
				continue
			}

			key := refKey{entityName: col.RefEntity, id: refVal.ID}
			if seen[key] {
				continue
			}
			seen[key] = true

			parsedID, err := id.Parse(refVal.ID)
			if err != nil {
				delete(m.Data, col.Key)
				continue
			}
			requests = append(requests, domain.RefResolveRequest{
				RefType: col.RefEntity,
				RefID:   parsedID,
			})
		}
	}

	if len(requests) == 0 {
		maskMovementsWithoutUnits(movements)
		return nil
	}

	// Batch resolve
	results, err := resolver.ResolveRefs(ctx, requests)
	if err != nil {
		return err
	}

	// Build lookup: (entityName, id) → resolved name
	resolved := make(map[refKey]domain.RefResolveResult)
	for _, r := range results {
		if r.Presentation != "" {
			resolved[refKey{entityName: r.RefType, id: r.RefID.String()}] = r
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
				delete(m.Data, col.Key)
				continue
			}
			if col.RefEntity == "" {
				delete(m.Data, col.Key)
				continue
			}

			key := refKey{entityName: col.RefEntity, id: refVal.ID}
			if ref, found := resolved[key]; found {
				refVal.Name = ref.Presentation
				refVal.CurrencyCode = ref.CurrencyCode
				refVal.DecimalPlaces = ref.DecimalPlaces
				refVal.URL = movementRefURL(ref, refVal.ID)
				movements[i].Data[col.Key] = refVal
			} else {
				delete(movements[i].Data, col.Key)
			}
		}
	}
	maskMovementsWithoutUnits(movements)
	return nil
}

// Missing or hidden unit metadata cannot silently fall back to scale 2.
func maskMovementsWithoutUnits(rows []entity.DocumentMovement) {
	for _, row := range rows {
		for _, column := range row.Columns {
			if column.Type != "amount" {
				continue
			}
			ref, ok := row.Data[column.CurrencyRef].(entity.MovementRefValue)
			if !ok || ref.DecimalPlaces == nil || ref.CurrencyCode == "" {
				delete(row.Data, column.Key)
			}
		}
	}
}
