package metadata

import (
	"strings"

	"metapus/internal/infrastructure/cache"
)

// EntityType defines the category of the entity.
type EntityType string

const (
	TypeCatalog  EntityType = "catalog"
	TypeDocument EntityType = "document"
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
// PreviewFieldDef describes a field that should appear in document preview cards
// (e.g. hover card in Related Documents). Auto-populated by Inspect() for all
// reference fields in the document header. Can be overridden via meta:"preview:false".
type PreviewFieldDef struct {
	Name          string `json:"name"`          // JSON field name, e.g. "supplierId"
	Label         string `json:"label"`         // Human label, e.g. "Поставщик"
	Column        string `json:"column"`        // DB column name, e.g. "supplier_id"
	ReferenceType string `json:"referenceType"` // e.g. "supplier" — resolves via cat_{plural}.name
}

// EntityDef describes a business entity.
type EntityDef struct {
	Name         string         `json:"name"`
	Key          string         `json:"key"`             // snake_case identifier, e.g. "counterparty", "goods_receipt"
	Type         EntityType     `json:"type"`
	Presentation Presentation   `json:"presentation"`          // rich display names
	RoutePrefix  string         `json:"routePrefix,omitempty"` // URL path segment, e.g. "counterparties"
	TableName    string         `json:"-"`
	Fields       []FieldDef     `json:"fields"`
	TableParts   []TablePartDef `json:"tableParts,omitempty"`

	// PreviewFields defines which fields appear in hover preview cards.
	// Auto-populated by Inspect(): all reference fields from document header
	// (except parent, organization — org is always shown via self).
	PreviewFields []PreviewFieldDef `json:"previewFields,omitempty"`

	// RefEndpoints maps referenceType → API endpoint path for filter UI.
	// E.g. "warehouse" → "/catalog/warehouses".
	// Set via SetRefEndpoints(); used by ToFilterMeta().
	RefEndpoints map[string]string `json:"-"`

	// CustomFields are dynamically-defined fields from sys_custom_field_schemas,
	// merged at runtime via MergeCustomFields(). They extend core Fields.
	CustomFields []FieldDef `json:"customFields,omitempty"`
}

// TablePartDef describes a nested collection (lines).
type TablePartDef struct {
	Name    string     `json:"name"`
	Label   string     `json:"label,omitempty"`
	Columns []FieldDef `json:"columns"`
}

// FieldDef describes a field.
type FieldDef struct {
	Name          string    `json:"name"`
	Label         string    `json:"label,omitempty"`
	Type          FieldType `json:"type"`
	ReferenceType string    `json:"referenceType,omitempty"` // For references, e.g. "warehouse"
	Required      bool      `json:"required,omitempty"`
	ReadOnly      bool      `json:"readOnly,omitempty"`
	Scale         int       `json:"scale,omitempty"` // For numbers
	Options       []string  `json:"options,omitempty"`

	// AllowedRefTypes lists permitted entity types for typed_ref fields.
	// Can include both document types ("CashReceipt") and catalog types ("Counterparty").
	// E.g. ["CashReceipt", "CashPayment"] for a bank statement line.
	// nil/empty = any registered entity type ("Произвольный" in 1C terms).
	// Only set when Type == TypeTypedRef.
	AllowedRefTypes []string `json:"allowedRefTypes,omitempty"`

	// ValueScale is the storage multiplier for filter value conversion.
	// User-visible values are multiplied by this before DB comparison.
	// E.g. Quantity (×10000), MinorUnits/Money (×100).
	// Array value scale parsing, or defaults to 0
	ValueScale int `json:"valueScale,omitempty"`

	// Dropdown choices and label hints strictly for the frontend Filter Sidebar Select component
	EnumValues []EnumValue `json:"enumValues,omitempty"`
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
	ValueScale    int               `json:"valueScale,omitempty"`    // storage multiplier
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
		if skipFilterFields[f.Name] {
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
			if skipFilterFields[col.Name] {
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
		if skipFilterFields[f.Name] {
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

// MergeCustomFields loads custom field schemas from SchemaCache and appends them
// as additional FieldDef entries. This merges CODE IS METADATA (Go structs) with
// runtime-defined custom fields (sys_custom_field_schemas).
//
// Call this after Inspect() during registry setup, or lazily on each request.
func (d *EntityDef) MergeCustomFields(sc *cache.SchemaCache) {
	if sc == nil {
		return
	}
	customs := sc.GetCustomFields(d.Key)
	if len(customs) == 0 {
		return
	}

	merged := make([]FieldDef, 0, len(customs))
	for _, cf := range customs {
		fd := FieldDef{
			Name:     "attributes." + cf.FieldName,
			Label:    cf.DisplayName,
			Type:     mapCustomFieldType(cf.FieldType),
			Required: cf.IsRequired,
			Options:  cf.EnumValues,
		}
		if cf.FieldType == "reference" {
			fd.ReferenceType = cf.ReferenceType
			if d.RefEndpoints != nil {
				fd.ReferenceType = cf.ReferenceType
			}
		}
		merged = append(merged, fd)
	}
	d.CustomFields = merged
}

// mapCustomFieldType maps sys_custom_field_schemas.field_type → metadata.FieldType.
func mapCustomFieldType(customType string) FieldType {
	switch customType {
	case "string", "text":
		return TypeString
	case "integer":
		return TypeInteger
	case "decimal":
		return TypeNumber
	case "boolean":
		return TypeBoolean
	case "date", "datetime":
		return TypeDate
	case "reference":
		return TypeReference
	case "enum":
		return TypeEnum
	case "json":
		return TypeString // fallback
	default:
		return TypeString
	}
}

// GenerateMock builds a sample JSON-compatible map with realistic placeholder values
// for each field based on its FieldType. Used by the CEL sandbox to auto-populate
// sample documents so users don't have to write JSON manually.
func (d *EntityDef) GenerateMock() map[string]interface{} {
	mock := make(map[string]interface{})
	for _, f := range d.Fields {
		mock[f.Name] = mockValue(f)
	}
	for _, tp := range d.TableParts {
		row := make(map[string]interface{})
		for _, col := range tp.Columns {
			row[col.Name] = mockValue(col)
		}
		mock[tp.Name] = []interface{}{row}
	}
	return mock
}

func mockValue(f FieldDef) interface{} {
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
	// Fallback: lookup by key (snake_case, e.g. "goods_receipt")
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

// GetEntityByRefType resolves a reference type (e.g. "supplier", "warehouse")
// to its entity name (e.g. "Counterparty", "Warehouse").
// Uses the refMappings registered via RegisterReferenceMapping().
func (r *Registry) GetEntityByRefType(refType string) (string, bool) {
	name, ok := r.refMappings[refType]
	return name, ok
}
