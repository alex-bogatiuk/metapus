// Package postgres provides PostgreSQL repository implementations.
package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/metadata"
	"metapus/pkg/logger"
)

// RefResolverRepo resolves typed references by querying entity tables.
// Uses metadata.Registry to map EntityName → table + entity type,
// then queries for presentation fields (number+date for documents, code+name for catalogs).
type RefResolverRepo struct {
	registry *metadata.Registry
}

// NewRefResolverRepo creates a new RefResolverRepo.
func NewRefResolverRepo(registry *metadata.Registry) *RefResolverRepo {
	return &RefResolverRepo{registry: registry}
}

// ResolveRefs resolves a batch of typed references into presentations.
// Groups refs by type for efficient batch querying (avoids N+1).
func (r *RefResolverRepo) ResolveRefs(ctx context.Context, refs []domain.RefResolveRequest) ([]domain.RefResolveResult, error) {
	if len(refs) == 0 {
		return nil, nil
	}

	// Group requests by refType for batch resolution per table
	byType := make(map[string][]int) // refType → indices in refs slice
	for i, ref := range refs {
		byType[ref.RefType] = append(byType[ref.RefType], i)
	}

	results := make([]domain.RefResolveResult, len(refs))
	for i, ref := range refs {
		results[i] = domain.RefResolveResult{
			RefType: ref.RefType,
			RefID:   ref.RefID,
		}
	}

	querier := MustGetTxManager(ctx).GetQuerier(ctx)

	for refType, indices := range byType {
		def, ok := r.registry.Get(refType)
		if !ok {
			logger.Warn(ctx, "RefResolverRepo: entity not found in registry", "refType", refType)
			continue
		}

		entityType := string(def.Type)
		tableName := deriveTableName(def)
		if tableName == "" {
			logger.Warn(ctx, "RefResolverRepo: empty table name", "refType", refType, "entityKey", def.Key)
			continue
		}


		// Collect unique IDs for this type
		ids := make([]id.ID, len(indices))
		for i, idx := range indices {
			ids[i] = refs[idx].RefID
		}

		// Build IN-clause
		placeholders := make([]string, len(ids))
		args := make([]any, len(ids))
		for i, uid := range ids {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = uid
		}
		inClause := strings.Join(placeholders, ",")

		singular := def.Presentation.Singular
		if singular == "" {
			singular = def.Name
		}

		// Query presentation based on entity type
		switch entityType {
		case "document":
			hasAmount := false
			hasCurrency := false
			for _, f := range def.Fields {
				if f.Name == "totalAmount" {
					hasAmount = true
				}
				if f.Name == "currencyId" {
					hasCurrency = true
				}
			}

			// Build SELECT with JOIN for preview fields
			selectCols := "d.id, d.number, d.date, d.posted, d.deletion_mark"
			if hasAmount {
				selectCols += ", d.total_amount"
			}
			if hasCurrency {
				selectCols += ", d.currency_id"
			}

			// Dynamic JOINs for preview fields (e.g. supplier_id → cat_counterparties.name)
			type previewJoin struct {
				alias string
				label string
			}
			joins := make([]string, 0, len(def.PreviewFields))
			previewJoins := make([]previewJoin, 0, len(def.PreviewFields))

			for i, pf := range def.PreviewFields {
				if pf.ReferenceType == "" {
					continue
				}
				// Resolve reference type to entity name → table name
				refEntityName, ok := r.registry.GetEntityByRefType(pf.ReferenceType)
				if !ok {
					continue
				}
				refDef, ok := r.registry.Get(refEntityName)
				if !ok {
					continue
				}
				refTable := deriveTableName(refDef)
				if refTable == "" {
					continue
				}

				alias := fmt.Sprintf("pv%d", i)
				selectCols += fmt.Sprintf(", %s.name AS %s_name", alias, alias)
				joins = append(joins, fmt.Sprintf(
					"LEFT JOIN %s %s ON %s.id = d.%s",
					refTable, alias, alias, pf.Column,
				))
				previewJoins = append(previewJoins, previewJoin{alias: alias, label: pf.Label})
			}

			fromClause := tableName + " d"
			joinClause := ""
			if len(joins) > 0 {
				joinClause = " " + strings.Join(joins, " ")
			}

			sql := fmt.Sprintf(
				`SELECT %s FROM %s%s WHERE d.id IN (%s)`,
				selectCols, fromClause, joinClause, inClause,
			)
			rows, err := querier.Query(ctx, sql, args...)
			if err != nil {
				logger.Error(ctx, "RefResolverRepo: document query failed", "error", err, "sql", sql)
				continue // skip this type on error, don't fail batch
			}

			type docFields struct {
				Number       string
				Date         time.Time
				Posted       bool
				DeletionMark bool
				Amount       int64
				CurrencyID   *id.ID
				PreviewData  map[string]string
			}
			presentations := make(map[id.ID]docFields)
			for rows.Next() {
				var uid id.ID
				var fields docFields
				var amount *int64
				var currencyID *id.ID

				scanDests := []any{&uid, &fields.Number, &fields.Date, &fields.Posted, &fields.DeletionMark}
				if hasAmount {
					scanDests = append(scanDests, &amount)
				}
				if hasCurrency {
					scanDests = append(scanDests, &currencyID)
				}

				// Scan preview field names (use **string to handle NULL from LEFT JOINs)
				previewNames := make([]*string, len(previewJoins))
				for i := range previewJoins {
					scanDests = append(scanDests, &previewNames[i])
				}

				if err := rows.Scan(scanDests...); err != nil {
					logger.Error(ctx, "RefResolverRepo: scan failed", "error", err, "tableName", tableName)
					continue
				}
				
				if hasAmount && amount != nil {
					fields.Amount = *amount
				}
				if hasCurrency && currencyID != nil {
					fields.CurrencyID = currencyID
				}

				// Build preview data map
				if len(previewJoins) > 0 {
					fields.PreviewData = make(map[string]string, len(previewJoins))
					for i, pj := range previewJoins {
						if previewNames[i] != nil && *previewNames[i] != "" {
							fields.PreviewData[pj.label] = *previewNames[i]
						}
					}
				}
				
				presentations[uid] = fields
			}
			rows.Close()

			for _, idx := range indices {
				results[idx].EntityType = entityType
				if f, ok := presentations[refs[idx].RefID]; ok {
					results[idx].Presentation = fmt.Sprintf("%s %s от %s", singular, f.Number, f.Date.Format("02.01.2006"))
					results[idx].Number = f.Number
					results[idx].Date = f.Date
					results[idx].Posted = f.Posted
					results[idx].DeletionMark = f.DeletionMark
					results[idx].Amount = f.Amount
					results[idx].CurrencyID = f.CurrencyID
					results[idx].PreviewData = f.PreviewData
				}
			}


		case "catalog":
			sql := fmt.Sprintf(
				`SELECT id, code, name FROM %s WHERE id IN (%s)`,
				tableName, inClause,
			)
			rows, err := querier.Query(ctx, sql, args...)
			if err != nil {
				continue
			}

			presentations := make(map[id.ID]string)
			for rows.Next() {
				var uid id.ID
				var code, name string
				if err := rows.Scan(&uid, &code, &name); err != nil {
					continue
				}
				if code != "" {
					presentations[uid] = fmt.Sprintf("%s (%s)", name, code)
				} else {
					presentations[uid] = name
				}
			}
			rows.Close()

			for _, idx := range indices {
				results[idx].EntityType = entityType
				if p, ok := presentations[refs[idx].RefID]; ok {
					results[idx].Presentation = p
				}
			}
		}
	}

	return results, nil
}

// deriveTableName determines the SQL table name from EntityDef.
// Convention: catalogs → "cat_{key_plural}", documents → "doc_{key_plural}".
func deriveTableName(def metadata.EntityDef) string {
	if def.TableName != "" {
		return def.TableName
	}

	key := def.Key
	if key == "" {
		return ""
	}

	switch def.Type {
	case metadata.TypeCatalog:
		return "cat_" + pluralizeKey(key)
	case metadata.TypeDocument:
		return "doc_" + pluralizeKey(key)
	default:
		return ""
	}
}

// pluralizeKey applies simple English pluralization for known Metapus entity keys.
func pluralizeKey(key string) string {
	irregulars := map[string]string{
		"counterparty":  "counterparties",
		"nomenclature":  "nomenclature",
		"warehouse":     "warehouses",
		"currency":      "currencies",
		"organization":  "organizations",
		"unit":          "units",
		"vat_rate":      "vat_rates",
		"contract":      "contracts",
		"goods_receipt": "goods_receipts",
		"goods_issue":   "goods_issues",
	}

	if plural, ok := irregulars[key]; ok {
		return plural
	}

	if strings.HasSuffix(key, "s") || strings.HasSuffix(key, "x") || strings.HasSuffix(key, "z") {
		return key + "es"
	}
	if strings.HasSuffix(key, "y") && len(key) > 1 {
		c := key[len(key)-2]
		if c != 'a' && c != 'e' && c != 'i' && c != 'o' && c != 'u' {
			return key[:len(key)-1] + "ies"
		}
	}
	return key + "s"
}
