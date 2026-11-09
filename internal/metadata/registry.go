package metadata

import (
	"strings"
)

// EntityType defines the category of the entity.
type EntityType string

const (
	TypeCatalog  EntityType = "catalog"
	TypeDocument EntityType = "document"
	TypeRegister EntityType = "register"
)

// FieldType defines the data type of a field.
type FieldType string

const (
	TypeString    FieldType = "string"
	TypeText      FieldType = "text"    // multiline text
	TypeInteger   FieldType = "integer" // simplified to "integer" (JSON "number" usually)
	TypeNumber    FieldType = "number"  // float/decimal
	TypeDecimal   FieldType = "decimal" // high-precision decimal
	TypeBoolean   FieldType = "boolean"
	TypeDate      FieldType = "date"
	TypeDatetime  FieldType = "datetime"
	TypeReference FieldType = "reference"
	TypeTypedRef  FieldType = "typed_ref" // polymorphic reference (document_type + document_id)
	TypeEnum      FieldType = "enum"
	TypeMoney     FieldType = "money"
	TypeJSON      FieldType = "json" // arbitrary JSON
)

// Presentation holds the human-readable display names for an entity.
// Used by frontend to render titles, buttons, breadcrumbs, etc.
type Presentation struct {
	Singular string `json:"singular"`           // e.g. "Counterparty"
	Plural   string `json:"plural"`             // e.g. "Counterparties"
	NewLabel string `json:"new,omitempty"`      // e.g. "New Counterparty"
	Genitive string `json:"genitive,omitempty"` // e.g. "counterparty" (for delete confirmations)
}

// DocumentCapabilities describes which generic document operations are
// available for an entity. A document can be metadata-driven without exposing
// the full CRUD/posting surface.
type DocumentCapabilities struct {
	List                bool `json:"list"`
	Read                bool `json:"read"`
	Create              bool `json:"create"`
	Update              bool `json:"update"`
	Delete              bool `json:"delete"`
	SetDeletionMark     bool `json:"setDeletionMark"`
	Post                bool `json:"post"`
	Unpost              bool `json:"unpost"`
	Repost              bool `json:"repost"`
	Copy                bool `json:"copy"`
	BatchAction         bool `json:"batchAction"`
	BatchActionByFilter bool `json:"batchActionByFilter"`
	ExportList          bool `json:"exportList"`
	PrintForms          bool `json:"printForms"`
	RelatedDocuments    bool `json:"relatedDocuments"`
	Movements           bool `json:"movements"`
}

// EntityActionScope tells generic consumers whether an action belongs to an
// individual item or to the whole collection.
type EntityActionScope string

const (
	EntityActionScopeItem       EntityActionScope = "item"
	EntityActionScopeCollection EntityActionScope = "collection"
)

// EntityActionUI contains explicit generic UI placements. Omitted/false values
// mean the action is discoverable in metadata but not rendered automatically.
type EntityActionUI struct {
	ListRow     bool `json:"listRow,omitzero"`
	FormToolbar bool `json:"formToolbar,omitzero"`
	ToolbarMenu bool `json:"toolbarMenu,omitzero"`
}

type EntityActionConfirmation struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	ConfirmLabel string `json:"confirmLabel"`
	CancelLabel  string `json:"cancelLabel"`
}

// EntityActionInput describes supported typed command inputs, not arbitrary
// client-generated payloads. reasonedDecision sends only {version, reason}.
type EntityActionInput struct {
	Kind           string   `json:"kind"`
	SummaryFields  []string `json:"summaryFields,omitempty"`
	ReasonMaxBytes int      `json:"reasonMaxBytes"`
}

// EntityActionDef describes an explicit custom operation in the same contract
// as standard document capabilities.
type EntityActionDef struct {
	Key                     string                    `json:"key"`
	Label                   string                    `json:"label"`
	Method                  string                    `json:"method"`
	Path                    string                    `json:"path"`
	Scope                   EntityActionScope         `json:"scope"`
	Permission              string                    `json:"permission,omitempty"`
	Idempotent              bool                      `json:"idempotent,omitzero"`
	Destructive             bool                      `json:"destructive,omitzero"`
	AllowedStates           []string                  `json:"allowedStates,omitempty"`
	RequiresSystemAdmin     bool                      `json:"requiresSystemAdmin,omitzero"`
	RequiredProtectionState string                    `json:"requiredProtectionState,omitempty"`
	Confirmation            *EntityActionConfirmation `json:"confirmation,omitempty"`
	UI                      *EntityActionUI           `json:"ui,omitempty"`
	Input                   *EntityActionInput        `json:"input,omitempty"`
}

// DocumentProtection describes a metadata-driven strict-document lock.
type DocumentProtection struct {
	LockFinalDocuments   bool     `json:"lockFinalDocuments"`
	UnlockField          string   `json:"unlockField"`
	MutableFields        []string `json:"mutableFields"`
	RequiresSystemAdmin  bool     `json:"requiresSystemAdmin"`
	AutoLockAfterPosting bool     `json:"autoLockAfterPosting"`
}

// PreviewFieldDef describes a field that should appear in document preview cards
// (e.g. hover card in Related Documents). Auto-populated by Inspect() for all
// reference fields in the document header. Can be overridden via meta:"preview:false".
type PreviewFieldDef struct {
	Name          string `json:"name"`          // JSON field name, e.g. "counterpartyId"
	Label         string `json:"label"`         // Human label, e.g. "Поставщик"
	Column        string `json:"column"`        // DB column name, e.g. "counterparty_id"
	ReferenceType string `json:"referenceType"` // e.g. "supplier" — resolves via cat_{plural}.name
}

// EntityDef describes a business entity.
type EntityDef struct {
	Name           string         `json:"name"`
	Key            string         `json:"key"` // snake_case identifier, e.g. "counterparty", "crypto_invoice"
	Type           EntityType     `json:"type"`
	Presentation   Presentation   `json:"presentation"`          // rich display names
	RoutePrefix    string         `json:"routePrefix,omitempty"` // URL path segment, e.g. "counterparties"
	TableName      string         `json:"-"`
	ReadPermission string         `json:"-"`
	ReadModel      any            `json:"-"`
	Fields         []FieldDef     `json:"fields"`
	TableParts     []TablePartDef `json:"tableParts,omitempty"`

	// Capabilities and Actions are populated for document entities. Catalogs
	// omit them because catalog CRUD is still a fixed generic contract.
	Capabilities *DocumentCapabilities `json:"capabilities,omitempty"`
	Actions      []EntityActionDef     `json:"actions,omitempty"`
	Protection   *DocumentProtection   `json:"protection,omitempty"`

	// PreviewFields defines which fields appear in hover preview cards.
	// Auto-populated by Inspect(): all reference fields from document header
	// (except parent, organization — org is always shown via self).
	PreviewFields []PreviewFieldDef `json:"previewFields,omitempty"`

	// RefEndpoints maps referenceType → API endpoint path for filter UI.
	// E.g. "merchant" → "/catalogs/merchants".
	// Set via SetRefEndpoints(); used by ToFilterMeta().
	RefEndpoints map[string]string `json:"-"`

	// CustomFields are dynamically-defined fields from sys_custom_field_schemas,
	// merged at runtime via MergeCustomFields(). They extend core Fields.
	CustomFields []FieldDef `json:"customFields,omitempty"`

	// RLSDimensions maps security dimension names to DB column names.
	// Used by global search to inject WHERE conditions from DataScope.
	// E.g. {"organization": "organization_id"}.
	// Populated via platform.RLSProvider during factory registration.
	RLSDimensions map[string]string `json:"-"`

	// SearchColumns configures which DB columns are searchable in global search.
	// If nil, defaults are used: catalogs → name+code, documents → number.
	// Populated via platform.SearchFieldsProvider during factory registration.
	SearchColumns *SearchColumns `json:"-"`
}

// SearchColumns mirrors platform.SearchFields for storage in EntityDef.
type SearchColumns struct {
	SearchCols  []string // DB columns for ILIKE matching
	TitleCol    string   // column for display title
	SubtitleCol string   // column for secondary text (empty = none)
}

// TablePartDef describes a nested collection (lines).
type TablePartDef struct {
	Name    string     `json:"name"`
	Label   string     `json:"label,omitempty"`
	Columns []FieldDef `json:"columns"`
}

// FieldDef describes a field.
type FieldDef struct {
	DBColumn      string    `json:"-"`
	Nullable      bool      `json:"-"`
	Name          string    `json:"name"`
	Label         string    `json:"label,omitempty"`
	Type          FieldType `json:"type"`
	ReferenceType string    `json:"referenceType,omitempty"` // For references, e.g. "merchant"
	Required      bool      `json:"required,omitzero"`
	ReadOnly      bool      `json:"readOnly,omitzero"`
	// WriteOnly renders an input in forms but prevents consumers from assuming
	// that the current value is returned by read APIs. Typical use: provider
	// credentials that can be replaced through the ordinary optimistic-locking
	// catalog update without echoing the stored value.
	WriteOnly bool     `json:"writeOnly,omitzero"`
	Scale     int      `json:"scale,omitzero"` // For numbers
	Options   []string `json:"options,omitempty"`

	// AllowedRefTypes lists permitted entity types for typed_ref fields.
	// Can include both document types ("CashReceipt") and catalog types ("Counterparty").
	// E.g. ["CashReceipt", "CashPayment"] for a bank statement line.
	// nil/empty = any registered entity type ("Произвольный" in 1C terms).
	// Only set when Type == TypeTypedRef.
	AllowedRefTypes []string `json:"allowedRefTypes,omitempty"`

	// ValueScale describes fixed-point quantities for the UI (Quantity ×10000).
	// Money is already a string of minor units and has no static multiplier.
	// A client copy of this hint never controls server-side filter conversion.
	ValueScale int `json:"valueScale,omitzero"`

	// Dropdown choices and label hints strictly for the frontend Filter Sidebar Select component
	EnumValues []EnumValue `json:"enumValues,omitempty"`

	// ListHidden excludes the field from list column choosers. The field remains
	// in GET /meta/:name so forms still render it. Set via meta:"list:false".
	ListHidden bool `json:"listHidden,omitzero"`
}

// FilterFieldMeta is a flat, frontend-compatible representation of a filterable field.
// Matches the frontend FilterFieldMeta interface in filter-config-dialog.tsx.
type FilterFieldMeta struct {
	Key           string            `json:"key"`
	Label         string            `json:"label"`
	FieldType     string            `json:"fieldType"` // string | number | date | boolean | reference | enum
	Group         string            `json:"group,omitempty"`
	RefEndpoint   string            `json:"refEndpoint,omitempty"`   // API path for reference fields
	RefEntityName string            `json:"refEntityName,omitempty"` // referenced entity name, e.g. "Counterparty"
	RefFields     []FilterFieldMeta `json:"refFields,omitempty"`     // filterable fields of the referenced entity
	ValueScale    int               `json:"valueScale,omitzero"`     // storage multiplier
	EnumValues    []EnumValue       `json:"enumValues,omitempty"`
}

// filterFieldType maps internal FieldType to the simplified frontend filter types.
func filterFieldType(ft FieldType) string {
	switch ft {
	case TypeString:
		return "string"
	case TypeInteger, TypeNumber:
		return "number"
	case TypeMoney:
		return "money"
	case TypeDate:
		return "date"
	case TypeBoolean:
		return "boolean"
	case TypeReference:
		return "reference"
	case TypeTypedRef:
		return "typed_ref"
	case TypeEnum:
		return "enum"
	default:
		return "string"
	}
}

// skipFilterFields are system/audit fields that should not appear in filter UI.
var skipFilterFields = map[string]bool{
	"id": true, "version": true, "attributes": true,
	"createdAt": true, "updatedAt": true,
	"createdBy": true, "updatedBy": true,
	"postedVersion": true,
	"txid":          true, "deletedAt": true,
}

// ToFilterMeta converts EntityDef into a flat list of FilterFieldMeta
// suitable for the frontend filter configuration dialog.
// If reg is provided, reference fields are eagerly enriched with
// RefEntityName and RefFields from the referenced entity.
func (d *EntityDef) ToFilterMeta(reg *Registry) []FilterFieldMeta {
	result := make([]FilterFieldMeta, 0, len(d.Fields))

	// Header fields (no group)
	for _, f := range d.Fields {
		if skipFilterFields[f.Name] || f.WriteOnly {
			continue
		}
		meta := FilterFieldMeta{
			Key:        f.Name,
			Label:      f.Label,
			FieldType:  filterFieldType(f.Type),
			ValueScale: f.ValueScale,
			EnumValues: f.EnumValues,
		}
		if f.Type == TypeReference && d.RefEndpoints != nil {
			meta.RefEndpoint = d.RefEndpoints[f.ReferenceType]
			// Eagerly inline ref entity fields
			if reg != nil {
				meta.enrichRefFields(f.ReferenceType, reg)
			}
		}
		result = append(result, meta)
	}

	// Table parts → each column becomes a filter field with group = table part label
	for _, tp := range d.TableParts {
		groupLabel := tp.Label
		for _, col := range tp.Columns {
			if skipFilterFields[col.Name] || col.WriteOnly {
				continue
			}
			meta := FilterFieldMeta{
				Key:        tp.Name + "." + col.Name,
				Label:      col.Label,
				FieldType:  filterFieldType(col.Type),
				Group:      groupLabel,
				ValueScale: col.ValueScale,
			}
			if col.Type == TypeReference && d.RefEndpoints != nil {
				meta.RefEndpoint = d.RefEndpoints[col.ReferenceType]
				if reg != nil {
					meta.enrichRefFields(col.ReferenceType, reg)
				}
			}
			result = append(result, meta)
		}
	}
	// Custom fields → each becomes a filter field with group = "Additional attributes"
	for _, cf := range d.CustomFields {
		meta := FilterFieldMeta{
			Key:       cf.Name,
			Label:     cf.Label,
			FieldType: filterFieldType(cf.Type),
			Group:     "Additional attributes",
		}
		if cf.Type == TypeReference && d.RefEndpoints != nil {
			meta.RefEndpoint = d.RefEndpoints[cf.ReferenceType]
		}
		result = append(result, meta)
	}

	return result
}

// enrichRefFields looks up the referenced entity in the registry and populates
// RefEntityName and RefFields with the referee’s own filterable scalar fields.
// Only non-reference, non-system fields are included (no recursive nesting).
func (m *FilterFieldMeta) enrichRefFields(refType string, reg *Registry) {
	// Look up entity name from reference mapping first
	entityName, ok := reg.refMappings[refType]
	if !ok {
		return
	}

	refDef, ok := reg.entities[entityName]
	if !ok {
		return
	}

	m.RefEntityName = entityName
	refFields := make([]FilterFieldMeta, 0)
	for _, f := range refDef.Fields {
		if skipFilterFields[f.Name] || f.WriteOnly {
			continue
		}
		// Only include scalar fields (not references — no recursive nesting)
		if f.Type == TypeReference {
			continue
		}
		refFields = append(refFields, FilterFieldMeta{
			Key:        f.Name,
			Label:      f.Label,
			FieldType:  filterFieldType(f.Type),
			ValueScale: f.ValueScale,
		})
	}
	if len(refFields) > 0 {
		m.RefFields = refFields
	}
}

// SetRefEndpoints configures the referenceType → API endpoint mapping.
// Called during registry setup so that ToFilterMeta() can emit refEndpoint for reference fields.
func (d *EntityDef) SetRefEndpoints(endpoints map[string]string) {
	d.RefEndpoints = endpoints
}

// MergeCustomFields installs independent descriptions supplied by an adapter.
// It replaces the previous snapshot, including removal of all custom fields.
func (d *EntityDef) MergeCustomFields(fields []FieldDef) {
	d.CustomFields = make([]FieldDef, len(fields))
	copy(d.CustomFields, fields)
	for i := range d.CustomFields {
		d.CustomFields[i].Options = append([]string(nil), fields[i].Options...)
	}
}

// GenerateMock builds a sample JSON-compatible map with realistic placeholder values
// for each field based on its FieldType. Used by the CEL sandbox to auto-populate
// sample documents so users don't have to write JSON manually.
func (d *EntityDef) GenerateMock() map[string]any {
	mock := make(map[string]any)
	for _, f := range d.Fields {
		mock[f.Name] = mockValue(f)
	}
	for _, tp := range d.TableParts {
		row := make(map[string]any)
		for _, col := range tp.Columns {
			row[col.Name] = mockValue(col)
		}
		mock[tp.Name] = []any{row}
	}
	return mock
}

func mockValue(f FieldDef) any {
	switch f.Type {
	case TypeString:
		if f.Name == "code" || f.Name == "number" {
			return "DOC-001"
		}
		if f.Name == "name" {
			return "Example"
		}
		if f.Name == "status" {
			return "draft"
		}
		return "example"
	case TypeInteger:
		return 1
	case TypeNumber:
		return 100.50
	case TypeMoney:
		return 5000000
	case TypeBoolean:
		if f.Name == "posted" || f.Name == "deletionMark" {
			return false
		}
		if f.Name == "isActive" {
			return true
		}
		return false
	case TypeDate:
		return "2025-01-15T10:00:00Z"
	case TypeReference:
		return "00000000-0000-0000-0000-000000000001"
	default:
		return nil
	}
}

// Registry stores entity definitions and reference type mappings.
type Registry struct {
	entities    map[string]EntityDef
	byKey       map[string]EntityDef // key (snake_case) → EntityDef for lookup by key
	refMappings map[string]string    // refType → entityName, e.g. "supplier" → "Counterparty"
}

func NewRegistry() *Registry {
	return &Registry{
		entities:    make(map[string]EntityDef),
		byKey:       make(map[string]EntityDef),
		refMappings: make(map[string]string),
	}
}

func (r *Registry) Register(def EntityDef) {
	r.entities[def.Name] = def
	if def.Key != "" {
		r.byKey[def.Key] = def
	}
}

// RegisterReferenceMapping maps a reference type to its entity name.
// E.g. RegisterReferenceMapping("supplier", "Counterparty").
// This is used by ToFilterMeta() to eagerly inline ref entity fields.
func (r *Registry) RegisterReferenceMapping(refType, entityName string) {
	r.refMappings[refType] = entityName
}

func (r *Registry) Get(name string) (EntityDef, bool) {
	if d, ok := r.entities[name]; ok {
		return d, true
	}
	// Fallback: lookup by key (snake_case, e.g. "crypto_invoice")
	if d, ok := r.byKey[name]; ok {
		return d, true
	}
	// Fallback: case-insensitive lookup
	lower := strings.ToLower(name)
	for k, d := range r.entities {
		if strings.ToLower(k) == lower {
			return d, true
		}
	}
	return EntityDef{}, false
}

func (r *Registry) List() []EntityDef {
	list := make([]EntityDef, 0, len(r.entities))
	for _, def := range r.entities {
		list = append(list, def)
	}
	return list
}

// GetEntityByRefType resolves a reference type (e.g. "supplier", "merchant")
// to its entity name (e.g. "Counterparty", "Merchant").
// Uses the refMappings registered via RegisterReferenceMapping().
func (r *Registry) GetEntityByRefType(refType string) (string, bool) {
	name, ok := r.refMappings[refType]
	return name, ok
}
