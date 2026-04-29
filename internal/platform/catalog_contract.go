package platform

import "metapus/internal/metadata"

// ---------------------------------------------------------------------------
// Optional interfaces — implement to provide richer metadata.
// Adding a new optional interface is a MINOR version bump (no breakage).
// The router checks for these via type assertion:
//
//	if p, ok := factory.(platform.Presentable); ok { ... }
// ---------------------------------------------------------------------------

// Presentable provides rich display names for the UI.
type Presentable interface {
	EntityPresentation() metadata.Presentation
}

// Inspectable provides a zero-value struct for metadata.Inspect().
type Inspectable interface {
	EntityStruct() interface{}
}

// Labeled provides a human-readable entity label.
type Labeled interface {
	EntityLabel() string
}

// ReferenceProvider declares which reference types this catalog satisfies.
// E.g. ["supplier", "customer"] for Counterparty.
type ReferenceProvider interface {
	ReferenceTypes() []string
}

// TableNameProvider allows a factory to explicitly specify its database table name.
// If not implemented, the table name is derived from RoutePrefix (e.g. cat_{routePrefix}).
type TableNameProvider interface {
	TableName() string
}

// RLSProvider declares row-level security dimensions for an entity.
// Dimension names map to DB column names (e.g. {"organization": "organization_id"}).
// Used by global search to inject WHERE conditions from DataScope.
// If not implemented, the entity has no RLS restrictions in global search.
type RLSProvider interface {
	RLSDimensions() map[string]string
}

// SearchFields configures which columns are searchable in Global Data Search.
type SearchFields struct {
	// SearchCols are the DB columns matched against the user query via ILIKE.
	// E.g. ["name", "code", "inn"] for counterparties, ["number"] for documents.
	SearchCols []string

	// TitleCol is the DB column used as the display title in search results.
	TitleCol string

	// SubtitleCol is the optional DB column shown as secondary text.
	// Empty string means no subtitle.
	SubtitleCol string
}

// SearchFieldsProvider allows a factory to declare which DB columns
// participate in global search. If not implemented, defaults apply:
//   - Catalogs: search by name + code, title = name, subtitle = code
//   - Documents: search by number, title = number, no subtitle
type SearchFieldsProvider interface {
	SearchableFields() SearchFields
}
