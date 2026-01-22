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
}

// Registry stores entity definitions.
type Registry struct {
	entities map[string]EntityDef
}

func NewRegistry() *Registry {
	return &Registry{
		entities: make(map[string]EntityDef),
	}
}

func (r *Registry) Register(def EntityDef) {
	r.entities[def.Name] = def
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
