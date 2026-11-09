package readaccess

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
	"currency":     {EntityName: "Currency", URLPrefix: "/catalogs/currencies"},
	"counterparty": {EntityName: "Counterparty", URLPrefix: "/catalogs/counterparties"},
	"contract":     {EntityName: "Contract", URLPrefix: "/catalogs/contracts"},
	"organization": {EntityName: "Organization", URLPrefix: "/catalogs/organizations"},
	// Crypto processing catalogs
	"merchant":        {EntityName: "Merchant", URLPrefix: "/catalogs/merchants"},
	"token":           {EntityName: "Token", URLPrefix: "/catalogs/tokens"},
	"wallet":          {EntityName: "Wallet", URLPrefix: "/catalogs/wallets"},
	"network":         {EntityName: "BlockchainNetwork", URLPrefix: "/catalogs/blockchain-networks"},
	"operationToken":  {EntityName: "Token", URLPrefix: "/catalogs/tokens"},
	"costToken":       {EntityName: "Token", URLPrefix: "/catalogs/tokens"},
	"providerAccount": {EntityName: "CryptoProviderAccount", URLPrefix: "/catalogs/crypto-provider-accounts"},
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
	var requests = make([]domain.RefResolveRequest, 0, len(refKeyToEntityName))

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
			mapping, mapped := refKeyToEntityName[col.Key]
			if !mapped {
				delete(m.Data, col.Key)
				continue
			}

			key := refKey{entityName: mapping.EntityName, id: refVal.ID}
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
				RefType: mapping.EntityName,
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
			mapping, mapped := refKeyToEntityName[col.Key]
			if !mapped {
				delete(m.Data, col.Key)
				continue
			}

			key := refKey{entityName: mapping.EntityName, id: refVal.ID}
			if ref, found := resolved[key]; found {
				refVal.Name = ref.Presentation
				refVal.CurrencyCode = ref.CurrencyCode
				refVal.DecimalPlaces = ref.DecimalPlaces
				refVal.URL = mapping.URLPrefix + "/" + refVal.ID
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
