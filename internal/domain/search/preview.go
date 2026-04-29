package search

import (
	"context"
	"fmt"
	"strings"

	"metapus/internal/infrastructure/storage/postgres"
)

// PreviewField is a single label:value pair in the preview card.
type PreviewField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// PreviewResponse is the API response for entity preview.
type PreviewResponse struct {
	EntityType string            `json:"entityType"`
	EntityKey  string            `json:"entityKey"`
	EntityName string            `json:"entityName"`
	Title      string            `json:"title"`
	Subtitle   string            `json:"subtitle,omitempty"`
	Fields     []PreviewField    `json:"fields"`
	References map[string]string `json:"references,omitempty"`
	URL        string            `json:"url"`
}

// Preview fetches a single entity's preview card data.
// Uses the entity's SearchableEntity metadata for title/subtitle and PreviewFields
// for additional scalar/reference fields.
func (s *Service) Preview(ctx context.Context, entityType, entityKey, entityID string) (*PreviewResponse, error) {
	// Find the entity in our searchable registry
	var entity *SearchableEntity
	for i := range s.entities {
		if s.entities[i].EntityKey == entityKey && s.entities[i].EntityType == entityType {
			entity = &s.entities[i]
			break
		}
	}
	if entity == nil {
		return nil, fmt.Errorf("entity not found: %s/%s", entityType, entityKey)
	}

	// Look up full EntityDef from metadata registry for PreviewFields
	def, hasDef := s.registry.Get(entityKey)

	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)

	// Build SELECT: title, subtitle, + scalar preview fields + reference LEFT JOINs
	selectCols := make([]string, 0, 8)
	selectCols = append(selectCols, "d."+entity.TitleCol+" AS title")
	if entity.SubtitleCol != "" {
		selectCols = append(selectCols, "COALESCE(d."+entity.SubtitleCol+", '') AS subtitle")
	} else {
		selectCols = append(selectCols, "'' AS subtitle")
	}

	// Document-specific columns
	if entityType == "document" {
		selectCols = append(selectCols, "d.date", "d.posted", "d.deletion_mark")
		// Conditionally add amount/currency
		if hasDef {
			for _, f := range def.Fields {
				if f.Name == "totalAmount" {
					selectCols = append(selectCols, "d.total_amount")
				}
				if f.Name == "currencyId" {
					selectCols = append(selectCols, "d.currency_id")
				}
			}
		}
	}

	// Scalar preview fields (INN, phone, email, etc.)
	type scalarPreview struct {
		column string
		label  string
	}
	var scalarPreviews []scalarPreview

	// Reference preview fields (counterparty_id → cat_counterparties.name)
	type refPreview struct {
		alias string
		label string
	}
	var refPreviews []refPreview
	var joins []string

	if hasDef {
		for i, pf := range def.PreviewFields {
			if pf.ReferenceType == "" {
				// Scalar field: select directly from main table
				selectCols = append(selectCols, "d."+pf.Column+" AS pv_scalar_"+pf.Column)
				scalarPreviews = append(scalarPreviews, scalarPreview{column: pf.Column, label: pf.Label})
			} else {
				// Reference field: LEFT JOIN
				refEntityName, ok := s.registry.GetEntityByRefType(pf.ReferenceType)
				if !ok {
					continue
				}
				refDef, ok := s.registry.Get(refEntityName)
				if !ok {
					continue
				}
				refTable := refDef.TableName
				if refTable == "" {
					continue
				}
				alias := fmt.Sprintf("pv%d", i)
				selectCols = append(selectCols, fmt.Sprintf("%s.name AS %s_name", alias, alias))
				joins = append(joins, fmt.Sprintf("LEFT JOIN %s %s ON %s.id = d.%s", refTable, alias, alias, pf.Column))
				refPreviews = append(refPreviews, refPreview{alias: alias, label: pf.Label})
			}
		}
	}

	fromClause := entity.TableName + " d"
	joinClause := ""
	if len(joins) > 0 {
		joinClause = " " + strings.Join(joins, " ")
	}

	sql := fmt.Sprintf(
		"SELECT %s FROM %s%s WHERE d.id = $1",
		strings.Join(selectCols, ", "), fromClause, joinClause,
	)

	rows, err := querier.Query(ctx, sql, entityID)
	if err != nil {
		return nil, fmt.Errorf("preview query: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("entity not found: %s/%s/%s", entityType, entityKey, entityID)
	}

	var title, subtitle string
	scanDests := []any{&title, &subtitle}

	// Document fields
	var date, posted, deletionMark any
	var totalAmount *int64
	var currencyID *string
	if entityType == "document" {
		scanDests = append(scanDests, &date, &posted, &deletionMark)
		if hasDef {
			for _, f := range def.Fields {
				if f.Name == "totalAmount" {
					scanDests = append(scanDests, &totalAmount)
				}
				if f.Name == "currencyId" {
					scanDests = append(scanDests, &currencyID)
				}
			}
		}
	}

	// Scalar previews
	scalarValues := make([]*string, len(scalarPreviews))
	for i := range scalarPreviews {
		scanDests = append(scanDests, &scalarValues[i])
	}

	// Reference previews
	refValues := make([]*string, len(refPreviews))
	for i := range refPreviews {
		scanDests = append(scanDests, &refValues[i])
	}

	if err := rows.Scan(scanDests...); err != nil {
		return nil, fmt.Errorf("preview scan: %w", err)
	}

	resp := &PreviewResponse{
		EntityType: entityType,
		EntityKey:  entityKey,
		EntityName: entity.EntityName,
		Title:      title,
		Subtitle:   subtitle,
		Fields:     make([]PreviewField, 0, len(scalarPreviews)+4),
		URL:        s.buildURL(searchRow{EntityType: entityType, RoutePrefix: entity.RoutePrefix, ID: entityID}),
	}

	// Add entity type presentation
	resp.Fields = append(resp.Fields, PreviewField{
		Label: "Тип",
		Value: entity.Presentation.Singular,
	})

	// Add document-specific fields
	if entityType == "document" {
		if date != nil {
			if t, ok := date.(interface{ Format(string) string }); ok {
				resp.Fields = append(resp.Fields, PreviewField{Label: "Дата", Value: t.Format("02.01.2006")})
			}
		}
		if posted != nil {
			if p, ok := posted.(bool); ok {
				if p {
					resp.Fields = append(resp.Fields, PreviewField{Label: "Статус", Value: "Проведён"})
				} else {
					resp.Fields = append(resp.Fields, PreviewField{Label: "Статус", Value: "Черновик"})
				}
			}
		}
		if deletionMark != nil {
			if dm, ok := deletionMark.(bool); ok && dm {
				resp.Fields = append(resp.Fields, PreviewField{Label: "Статус", Value: "Удалён"})
			}
		}
		if totalAmount != nil {
			resp.Fields = append(resp.Fields, PreviewField{
				Label: "Сумма",
				Value: fmt.Sprintf("%d", *totalAmount), // MinorUnits — frontend will format
			})
		}
	}

	// Add scalar preview fields (INN, phone, email, etc.)
	for i, sp := range scalarPreviews {
		if scalarValues[i] != nil && *scalarValues[i] != "" {
			resp.Fields = append(resp.Fields, PreviewField{
				Label: sp.label,
				Value: *scalarValues[i],
			})
		}
	}

	// Add reference preview fields
	if len(refPreviews) > 0 {
		resp.References = make(map[string]string, len(refPreviews))
		for i, rp := range refPreviews {
			if refValues[i] != nil && *refValues[i] != "" {
				resp.References[rp.label] = *refValues[i]
			}
		}
	}

	return resp, nil
}
