// Package search provides global data search across all registered entities.
// Authorization is applied to candidate rows before forming presentations.
package search

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/alex-bogatiuk/metapus/internal/application/readaccess"
	"github.com/alex-bogatiuk/metapus/internal/core/apperror"

	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

// _minQueryLength is the minimum number of runes to trigger a search.
const _minQueryLength = 2

// _maxLimitPerEntity caps the per-entity LIMIT to prevent abuse.
const _maxLimitPerEntity = 10

// SearchableEntity describes one entity source for global search.
type SearchableEntity struct {
	EntityType    string            // "catalog" / "document"
	EntityName    string            // "Counterparty"
	EntityKey     string            // "counterparty"
	TableName     string            // "cat_counterparties"
	SearchCols    []string          // ["name", "code"] or ["number"]
	TitleCol      string            // column for display title
	SubtitleCol   string            // optional secondary column (empty = no subtitle)
	RoutePrefix   string            // "counterparties", "goods-receipt"
	RLSDimensions map[string]string // {"organization": "organization_id"}
	Presentation  metadata.Presentation
}

// SearchResult is a single matched row.
type SearchResult struct {
	EntityType string `json:"entityType"`
	EntityName string `json:"entityName"`
	EntityKey  string `json:"entityKey"`
	EntityID   string `json:"entityId"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle,omitempty"`
	URL        string `json:"url"`
}

// SearchResponse is the API response.
type SearchResponse struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

// Service performs global search across registered entities.
type Service struct {
	entities []SearchableEntity
	registry *metadata.Registry
	reads    *readaccess.Service
}

// NewService creates a search service from the metadata registry.
// Searchable entities are resolved once at startup (immutable metadata).
func NewService(reg *metadata.Registry, reads *readaccess.Service) *Service {
	defs := reg.List()
	entities := make([]SearchableEntity, 0, len(defs))

	for _, def := range defs {
		if def.TableName == "" {
			continue
		}

		se := SearchableEntity{
			EntityType:    string(def.Type),
			EntityName:    def.Name,
			EntityKey:     def.Key,
			TableName:     def.TableName,
			RoutePrefix:   def.RoutePrefix,
			RLSDimensions: def.RLSDimensions,
			Presentation:  def.Presentation,
		}

		// Use entity-specific search columns if provided, otherwise fall back to defaults
		if def.SearchColumns != nil {
			se.SearchCols = def.SearchColumns.SearchCols
			se.TitleCol = def.SearchColumns.TitleCol
			se.SubtitleCol = def.SearchColumns.SubtitleCol
		} else {
			switch def.Type {
			case metadata.TypeCatalog:
				se.SearchCols = []string{"name", "code"}
				se.TitleCol = "name"
				se.SubtitleCol = "code"
			case metadata.TypeDocument:
				se.SearchCols = []string{"number"}
				se.TitleCol = "number"
				se.SubtitleCol = ""
			default:
				continue
			}
		}

		entities = append(entities, se)
	}

	return &Service{entities: entities, registry: reg, reads: reads}
}

// searchRow mirrors the SELECT columns of each UNION ALL sub-select.
// Fields are scanned by pgxscan in column order.
type searchRow struct {
	ID          string `db:"id"`
	Title       string `db:"title"`
	Subtitle    string `db:"subtitle"`
	EntityType  string `db:"entity_type"`
	EntityName  string `db:"entity_name"`
	EntityKey   string `db:"entity_key"`
	RoutePrefix string `db:"route_prefix"`
}

// Search executes a global search across all registered entities.
// Returns results grouped by entity type.
func (s *Service) Search(ctx context.Context, query string, limitPerEntity int) (*SearchResponse, error) {
	if utf8.RuneCountInString(query) < _minQueryLength {
		return &SearchResponse{Query: query}, nil
	}

	if limitPerEntity <= 0 || limitPerEntity > _maxLimitPerEntity {
		limitPerEntity = 5
	}

	results := make([]SearchResult, 0)
	for _, entity := range s.entities {
		if _, err := s.reads.Require(ctx, entity.EntityKey); err != nil {
			if apperror.GetHTTPStatus(err) == http.StatusForbidden {
				continue
			}
			return nil, err
		}
		rows, err := s.reads.Search(ctx, entity.EntityKey, query, entity.SearchCols, limitPerEntity)
		if err != nil {
			return nil, err
		}
		def, _ := s.registry.Get(entity.EntityKey)
		for _, row := range rows {
			title := projectionText(def, row, entity.TitleCol)
			if title == "" {
				title = entity.Presentation.Singular
			}
			results = append(results, SearchResult{
				EntityType: entity.EntityType, EntityName: entity.EntityName, EntityKey: entity.EntityKey,
				EntityID: row.ID.String(), Title: title, Subtitle: projectionText(def, row, entity.SubtitleCol),
				URL: s.buildURL(searchRow{EntityType: entity.EntityType, RoutePrefix: entity.RoutePrefix, ID: row.ID.String()}),
			})
		}
	}
	return &SearchResponse{Query: query, Results: results}, nil
}

func projectionText(def metadata.EntityDef, row readaccess.Projection, column string) string {
	for _, field := range def.Fields {
		if field.DBColumn == column && column != "" {
			if value := row.Fields[field.Name]; value != nil {
				return fmt.Sprint(value)
			}
		}
	}
	return ""
}

// buildURL constructs the frontend URL for a search result.
// RoutePrefix is singular on the backend (e.g. "goods-receipt") but
// Next.js routes use plural form (e.g. "/documents/goods-receipts").
func (s *Service) buildURL(row searchRow) string {
	prefix := pluralizePrefix(row.RoutePrefix)
	switch row.EntityType {
	case "catalog":
		return "/catalogs/" + prefix + "/" + row.ID
	case "document":
		return "/documents/" + prefix + "/" + row.ID
	default:
		return "/" + prefix + "/" + row.ID
	}
}

// pluralizePrefix appends "s" if the prefix doesn't already end with one.
// Mirrors frontend/lib/entity-url.ts → pluralizePrefix().
func pluralizePrefix(p string) string {
	if strings.HasSuffix(p, "s") {
		return p
	}
	return p + "s"
}
