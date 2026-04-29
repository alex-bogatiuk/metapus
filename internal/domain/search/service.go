// Package search provides global data search across all registered entities.
// Uses a single UNION ALL query for minimal round-trips.
package search

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/security"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/metadata"
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
	EntityType  string `json:"entityType"`
	EntityName  string `json:"entityName"`
	EntityKey   string `json:"entityKey"`
	EntityID    string `json:"entityId"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	URL         string `json:"url"`
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
}

// NewService creates a search service from the metadata registry.
// Searchable entities are resolved once at startup (immutable metadata).
func NewService(reg *metadata.Registry) *Service {
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

	return &Service{entities: entities, registry: reg}
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

	scope := security.GetDataScope(ctx)
	pattern := "%" + escapeLikePattern(query) + "%"

	// Build UNION ALL from all searchable entities.
	var parts []string
	args := []any{pattern} // $1 = ILIKE pattern
	paramIdx := 2          // next placeholder index

	for _, e := range s.entities {
		subSQL, subArgs, nextIdx := s.buildSubSelect(e, scope, paramIdx, limitPerEntity)
		if subSQL == "" {
			continue // entity excluded by RLS (empty dimension = no access)
		}
		parts = append(parts, subSQL)
		args = append(args, subArgs...)
		paramIdx = nextIdx
	}

	if len(parts) == 0 {
		return &SearchResponse{Query: query}, nil
	}

	fullSQL := strings.Join(parts, " UNION ALL ")

	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var rows []searchRow
	if err := pgxscan.Select(ctx, querier, &rows, fullSQL, args...); err != nil {
		return nil, fmt.Errorf("global search: %w", err)
	}

	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		url := s.buildURL(row)
		results = append(results, SearchResult{
			EntityType: row.EntityType,
			EntityName: row.EntityName,
			EntityKey:  row.EntityKey,
			EntityID:   row.ID,
			Title:      row.Title,
			Subtitle:   row.Subtitle,
			URL:        url,
		})
	}

	return &SearchResponse{Query: query, Results: results}, nil
}

// buildSubSelect generates one parenthesized SELECT for a single entity.
// Returns empty string if the entity is excluded by RLS.
func (s *Service) buildSubSelect(e SearchableEntity, scope *security.DataScope, paramIdx int, limit int) (string, []any, int) {
	// Build ILIKE conditions: (name ILIKE $1 OR code ILIKE $1)
	ilikeOrs := make([]string, 0, len(e.SearchCols))
	for _, col := range e.SearchCols {
		ilikeOrs = append(ilikeOrs, col+" ILIKE $1")
	}
	searchCond := "(" + strings.Join(ilikeOrs, " OR ") + ")"

	// Build WHERE clauses
	wheres := make([]string, 0, 4)

	// Catalogs have deletion_mark; documents don't need it for search
	if e.EntityType == "catalog" {
		wheres = append(wheres, "deletion_mark = false")
	}

	wheres = append(wheres, searchCond)

	// Apply RLS conditions from DataScope
	var extraArgs []any
	if scope != nil && !scope.IsAdmin && len(e.RLSDimensions) > 0 {
		effective := scope.EffectiveDimensions(e.EntityKey)
		for dimName, dbCol := range e.RLSDimensions {
			allowedIDs, hasDim := effective[dimName]
			if !hasDim {
				continue // no restriction on this dimension
			}
			if len(allowedIDs) == 0 {
				// Empty = no access → skip this entity entirely
				return "", nil, paramIdx
			}
			// Build IN ($N, $N+1, ...)
			placeholders := make([]string, 0, len(allowedIDs))
			for _, id := range allowedIDs {
				placeholders = append(placeholders, "$"+strconv.Itoa(paramIdx))
				extraArgs = append(extraArgs, id)
				paramIdx++
			}
			wheres = append(wheres, dbCol+" IN ("+strings.Join(placeholders, ", ")+")")
		}
	}

	whereClause := strings.Join(wheres, " AND ")

	// Subtitle column: use empty string if not defined
	subtitleExpr := "''"
	if e.SubtitleCol != "" {
		subtitleExpr = "COALESCE(" + e.SubtitleCol + ", '')"
	}

	// Order: catalogs by name, documents by date DESC
	orderBy := e.TitleCol
	if e.EntityType == "document" {
		orderBy = "date DESC"
	}

	subSQL := fmt.Sprintf(
		"(SELECT id::text, %s AS title, %s AS subtitle, '%s' AS entity_type, '%s' AS entity_name, '%s' AS entity_key, '%s' AS route_prefix FROM %s WHERE %s ORDER BY %s LIMIT %d)",
		e.TitleCol, subtitleExpr,
		e.EntityType, e.EntityName, e.EntityKey, e.RoutePrefix,
		e.TableName, whereClause, orderBy, limit,
	)

	return subSQL, extraArgs, paramIdx
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

// escapeLikePattern escapes SQL LIKE special characters.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
