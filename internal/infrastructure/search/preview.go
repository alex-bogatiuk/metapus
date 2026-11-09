package search

import (
	"context"
	"fmt"
	"metapus/internal/application/readaccess"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"

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

// Preview uses the same row authorization as search and reference resolution.
func (s *Service) Preview(ctx context.Context, entityType, entityKey, entityID string) (*PreviewResponse, error) {
	var entity *SearchableEntity
	for i := range s.entities {
		if s.entities[i].EntityKey == entityKey && s.entities[i].EntityType == entityType {
			entity = &s.entities[i]
			break
		}
	}
	if entity == nil {
		return nil, apperror.NewNotFound(entityKey, entityID)
	}
	uid, err := id.Parse(entityID)
	if err != nil {
		return nil, apperror.NewValidation("invalid entity id")
	}
	row, err := s.reads.ReadOne(ctx, entityKey, uid)
	if err != nil {
		return nil, err
	}
	def, _ := s.registry.Get(entityKey)
	title := projectionText(def, row, entity.TitleCol)
	if title == "" {
		title = entity.Presentation.Singular
	}
	resp := &PreviewResponse{
		EntityType: entityType, EntityKey: entityKey, EntityName: entity.EntityName,
		Title: title, Subtitle: projectionText(def, row, entity.SubtitleCol),
		Fields: []PreviewField{{Label: "Тип", Value: entity.Presentation.Singular}},
		URL:    s.buildURL(searchRow{EntityType: entityType, RoutePrefix: entity.RoutePrefix, ID: entityID}),
	}
	if entityType == "document" {
		if date, ok := row.Fields["date"].(interface{ Format(string) string }); ok {
			resp.Fields = append(resp.Fields, PreviewField{Label: "Дата", Value: date.Format("02.01.2006")})
		}
		if posted, ok := row.Fields["posted"].(bool); ok {
			status := "Черновик"
			if posted {
				status = "Проведён"
			}
			resp.Fields = append(resp.Fields, PreviewField{Label: "Статус", Value: status})
		}
		if deleted, ok := row.Fields["deletionMark"].(bool); ok && deleted {
			resp.Fields = append(resp.Fields, PreviewField{Label: "Статус", Value: "Удалён"})
		}
		money, err := s.reads.ReadMoney(ctx, []readaccess.Projection{row}, readaccess.MoneyFields{Amount: "totalAmount", CurrencyID: "currencyId", CurrencyEntity: "currency"})
		if err != nil {
			return nil, err
		}
		if amount, ok := money[row.ID]; ok {
			text, err := types.FormatMinorUnits(amount.Amount.MinorUnitsValue(), amount.Unit.DecimalPlaces)
			if err != nil {
				return nil, err
			}
			resp.Fields = append(resp.Fields, PreviewField{Label: "Сумма", Value: fmt.Sprintf("%s %s", text, amount.Unit.Code)})
		}
	}
	previews, err := postgres.AuthorizedPreviewData(ctx, s.registry, s.reads, def, []readaccess.Projection{row})
	if err != nil {
		return nil, err
	}
	for _, field := range def.PreviewFields {
		value := previews[uid][field.Label]
		if value == "" {
			continue
		}
		if field.ReferenceType == "" {
			resp.Fields = append(resp.Fields, PreviewField{Label: field.Label, Value: value})
		} else {
			if resp.References == nil {
				resp.References = make(map[string]string)
			}
			resp.References[field.Label] = value
		}
	}
	return resp, nil
}
