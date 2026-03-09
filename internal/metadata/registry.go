package metadata

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
	TypeInteger   FieldType = "integer" // simplified to "integer" (JSON "number" usually)
	TypeNumber    FieldType = "number"  // float/decimal
	TypeBoolean   FieldType = "boolean"
	TypeDate      FieldType = "date"
	TypeReference FieldType = "reference"
	TypeEnum      FieldType = "enum"
	TypeMoney     FieldType = "money"
)

// EntityDef describes a business entity.
type EntityDef struct {
	Name       string         `json:"name"`
	Label      string         `json:"label,omitempty"`
	Type       EntityType     `json:"type"`
	TableName  string         `json:"-"`
	Fields     []FieldDef     `json:"fields"`
	TableParts []TablePartDef `json:"tableParts,omitempty"`

	// RefEndpoints maps referenceType → API endpoint path for filter UI.
	// E.g. "warehouse" → "/catalog/warehouses".
	// Set via SetRefEndpoints(); used by ToFilterMeta().
	RefEndpoints map[string]string `json:"-"`
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

	// ValueScale is the storage multiplier for filter value conversion.
	// User-visible values are multiplied by this before DB comparison.
	// E.g. Quantity (×10000), MinorUnits/Money (×100).
	// 0 means no scaling.
	ValueScale int `json:"valueScale,omitempty"`
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



// Registry stores entity definitions and reference type mappings.
type Registry struct {
	entities    map[string]EntityDef
	refMappings map[string]string // refType → entityName, e.g. "supplier" → "Counterparty"
}

func NewRegistry() *Registry {
	return &Registry{
		entities:    make(map[string]EntityDef),
		refMappings: make(map[string]string),
	}
}

func (r *Registry) Register(def EntityDef) {
	r.entities[def.Name] = def
}

// RegisterReferenceMapping maps a reference type to its entity name.
// E.g. RegisterReferenceMapping("supplier", "Counterparty").
// This is used by ToFilterMeta() to eagerly inline ref entity fields.
func (r *Registry) RegisterReferenceMapping(refType, entityName string) {
	r.refMappings[refType] = entityName
}

func (r *Registry) Get(name string) (EntityDef, bool) {
	d, ok := r.entities[name]
	return d, ok
}

func (r *Registry) List() []EntityDef {
	list := make([]EntityDef, 0, len(r.entities))
	for _, def := range r.entities {
		list = append(list, def)
	}
	return list
}
