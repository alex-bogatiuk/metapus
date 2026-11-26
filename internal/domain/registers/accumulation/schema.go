// Package accumulation declares accumulation registers (регистры
// накопления) from their movement type. A configuration describes a register
// once: a movement struct that embeds entity.MovementBase and tags its
// dimensions and resources, and a Definition with the register's key, name,
// table and kind. The platform derives the rest from that description:
// storage of the movements, the balances their triggers keep, posting,
// reposting and unposting, the document's movements view, balance and
// turnover queries, and a check that the tenant database holds the tables
// the description promises.
//
//	type Movement struct {
//		entity.MovementBase
//		WarehouseID id.ID          `db:"warehouse_id" meta:"label:Склад,ref:warehouse" register:"dimension"`
//		ItemID      id.ID          `db:"item_id" meta:"label:Номенклатура,ref:item" register:"dimension"`
//		Quantity    types.Quantity `db:"quantity" meta:"label:Количество" register:"resource"`
//	}
//
//	var Register = accumulation.New[Movement](accumulation.Definition{
//		Key: "warehouse_stock", Label: "Запасы на складах", Table: "reg_warehouse_stock", Kind: accumulation.Balances,
//	})
package accumulation

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
)

// Kind is what a register keeps.
type Kind string

const (
	// Balances registers (остатки) keep a balance per combination of
	// dimensions: a receipt adds the resources, an expense takes them away.
	// Triggers on the movements table keep the balances table in step.
	Balances Kind = "balances"
	// Turnovers registers (обороты) only sum their movements over a period;
	// they have no balances table and every movement is a receipt.
	Turnovers Kind = "turnovers"
)

// Definition names a register.
type Definition struct {
	// Key identifies the register: its movements' field policy entity and
	// its place in a posting's movement set. Snake case, unique.
	Key string
	// Label is the register's name for people ("Запасы на складах").
	Label string
	// Table is the prefix of the register's tables: movements are stored in
	// Table+"_movements" and, for Balances, balances in Table+"_balances".
	// It starts with "reg_".
	Table string
	// Kind is Balances or Turnovers.
	Kind Kind
	// NonNegative forbids a posting or unposting that takes a resource of a
	// Balances register below zero (контроль отрицательных остатков). The
	// platform locks the combinations the document moves, in one order for
	// every posting, and checks them once all registers are written. A
	// combination the document leaves below zero without lowering it, such
	// as a receipt into a balance that is already negative, passes.
	NonNegative bool
}

// FieldType is how a dimension or resource is stored and shown.
type FieldType string

const (
	// FieldRef is a reference to a catalog or document (id.ID, uuid).
	FieldRef FieldType = "ref"
	// FieldText is a string dimension (text).
	FieldText FieldType = "text"
	// FieldQuantity is a types.Quantity resource (bigint, 4 decimal places).
	FieldQuantity FieldType = "quantity"
	// FieldAmount is a types.MinorUnits resource (bigint in minor units of
	// the currency its Currency dimension names).
	FieldAmount FieldType = "amount"
)

// SQLType is the PostgreSQL type of the field's column.
func (t FieldType) SQLType() string {
	switch t {
	case FieldRef:
		return "uuid"
	case FieldText:
		return "text"
	default:
		return "bigint"
	}
}

// Field is a dimension or resource of a register.
type Field struct {
	// Column is the field's column, from its db tag.
	Column string
	// Label is the field's name for people, from meta:"label:…".
	Label string
	// Type is how the field is stored and shown.
	Type FieldType
	// Ref is the entity a FieldRef dimension refers to, from meta:"ref:…".
	Ref string
	// Currency is, for a FieldAmount resource, the column of the dimension
	// that holds the amount's currency (register:"resource,currency=…").
	Currency string
	index    []int
}

// Header columns are the movements' columns every register has, in the
// order they are stored, from entity.MovementBase.
var headerColumns = []string{"line_id", "recorder_id", "recorder_type", "recorder_version", "period", "record_type", "created_at"}

// Schema is a register's definition with the dimensions and resources of its
// movement type.
type Schema struct {
	Definition
	Dimensions []Field
	Resources  []Field
	movement   reflect.Type
}

// MovementsTable is the table of the register's movements.
func (s *Schema) MovementsTable() string { return s.Table + "_movements" }

// BalancesTable is the table of the register's balances; a Turnovers
// register has none.
func (s *Schema) BalancesTable() string {
	if s.Kind != Balances {
		return ""
	}
	return s.Table + "_balances"
}

// HeaderColumns are the movements' columns every register has.
func HeaderColumns() []string { return slices.Clone(headerColumns) }

// MovementColumns are the columns of the movements table in the order Values
// returns them: the header, the dimensions, the resources.
func (s *Schema) MovementColumns() []string {
	columns := slices.Clone(headerColumns)
	return append(columns, s.FieldColumns()...)
}

// FieldColumns are the dimensions' columns followed by the resources'.
func (s *Schema) FieldColumns() []string {
	return append(s.DimensionColumns(), s.ResourceColumns()...)
}

// DimensionColumns are the dimensions' columns.
func (s *Schema) DimensionColumns() []string { return columnsOf(s.Dimensions) }

// ResourceColumns are the resources' columns.
func (s *Schema) ResourceColumns() []string { return columnsOf(s.Resources) }

func columnsOf(fields []Field) []string {
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = f.Column
	}
	return columns
}

// IsDimension reports whether column is a dimension of the register.
func (s *Schema) IsDimension(column string) bool {
	return slices.ContainsFunc(s.Dimensions, func(f Field) bool { return f.Column == column })
}

// Values flattens movement, a pointer to the register's movement type, into
// the values of MovementColumns.
func (s *Schema) Values(movement Movement) []any {
	v := reflect.ValueOf(movement).Elem()
	h := movement.Header()
	values := []any{h.LineID, h.RecorderID, h.RecorderType, h.RecorderVersion, h.Period, h.RecordType, h.CreatedAt}
	for _, f := range s.Dimensions {
		values = append(values, v.FieldByIndex(f.index).Interface())
	}
	for _, f := range s.Resources {
		values = append(values, v.FieldByIndex(f.index).Interface())
	}
	return values
}

// dimensionValues are movement's dimensions in schema order.
func (s *Schema) dimensionValues(v reflect.Value) []any {
	values := make([]any, len(s.Dimensions))
	for i, f := range s.Dimensions {
		values[i] = v.FieldByIndex(f.index).Interface()
	}
	return values
}

// resourceValues are movement's resources in schema order, in their stored
// units.
func (s *Schema) resourceValues(v reflect.Value) []int64 {
	values := make([]int64, len(s.Resources))
	for i, f := range s.Resources {
		values[i] = v.FieldByIndex(f.index).Int()
	}
	return values
}

// Movement is a pointer to a register's movement: its type embeds
// entity.MovementBase.
type Movement interface {
	Header() *entity.MovementBase
}

var (
	keyPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	tablePattern   = regexp.MustCompile(`^reg_[a-z0-9_]+$`)
	columnPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	idType         = reflect.TypeFor[id.ID]()
	quantityType   = reflect.TypeFor[types.Quantity]()
	minorUnitsType = reflect.TypeFor[types.MinorUnits]()
	baseType       = reflect.TypeFor[entity.MovementBase]()
)

// NewSchema describes the register def whose movements have type movement.
// It reports every mistake of the description at once.
func NewSchema(def Definition, movement reflect.Type) (*Schema, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if !keyPattern.MatchString(def.Key) {
		add("key %q is not snake case", def.Key)
	}
	if strings.TrimSpace(def.Label) == "" {
		add("the label is empty")
	}
	if !tablePattern.MatchString(def.Table) {
		add("table %q does not start with reg_ or is not snake case", def.Table)
	}
	if def.Kind != Balances && def.Kind != Turnovers {
		add("kind %q is neither %q nor %q", def.Kind, Balances, Turnovers)
	}
	if def.NonNegative && def.Kind != Balances {
		add("a %s register has no balances to keep non-negative", def.Kind)
	}
	schema := &Schema{Definition: def, movement: movement}
	if movement.Kind() != reflect.Struct {
		add("movement type %s is not a struct", movement)
		return nil, describe(def, problems)
	}
	embedsBase := false
	seen := map[string]bool{}
	for _, c := range headerColumns {
		seen[c] = true
	}
	for i := range movement.NumField() {
		field := movement.Field(i)
		if field.Anonymous && field.Type == baseType {
			embedsBase = true
			continue
		}
		role, options := parseRegisterTag(field.Tag.Get("register"))
		column := strings.Split(field.Tag.Get("db"), ",")[0]
		if role == "" {
			if field.IsExported() && column != "" && column != "-" {
				add("field %s has a column but no register:\"dimension\" or register:\"resource\" tag", field.Name)
			}
			continue
		}
		if !field.IsExported() {
			add("field %s is not exported", field.Name)
			continue
		}
		if !columnPattern.MatchString(column) {
			add("field %s has no db column", field.Name)
			continue
		}
		if seen[column] {
			add("column %s of field %s is taken", column, field.Name)
			continue
		}
		seen[column] = true
		f := Field{Column: column, Label: metaValue(field, "label"), Ref: metaValue(field, "ref"), index: field.Index}
		if f.Label == "" {
			f.Label = field.Name
		}
		switch role {
		case "dimension":
			switch {
			case field.Type == idType:
				f.Type = FieldRef
				if f.Ref == "" {
					add("dimension %s refers to nothing: add meta:\"ref:<entity key>\"", field.Name)
				}
			case field.Type.Kind() == reflect.String:
				f.Type = FieldText
			default:
				add("dimension %s has type %s; a dimension is an id.ID or a string", field.Name, field.Type)
			}
			if len(options) > 0 {
				add("dimension %s takes no options", field.Name)
			}
			schema.Dimensions = append(schema.Dimensions, f)
		case "resource":
			switch field.Type {
			case quantityType:
				f.Type = FieldQuantity
			case minorUnitsType:
				f.Type = FieldAmount
			default:
				add("resource %s has type %s; a resource is a types.Quantity or types.MinorUnits", field.Name, field.Type)
			}
			for name, value := range options {
				if name != "currency" || f.Type != FieldAmount {
					add("resource %s: option %s is not known", field.Name, name)
					continue
				}
				f.Currency = value
			}
			schema.Resources = append(schema.Resources, f)
		default:
			add("field %s has register tag %q; it is dimension or resource", field.Name, role)
		}
	}
	if !embedsBase {
		add("movement type %s does not embed entity.MovementBase", movement)
	}
	if len(schema.Dimensions) == 0 {
		add("the register has no dimension")
	}
	if len(schema.Resources) == 0 {
		add("the register has no resource")
	}
	for _, r := range schema.Resources {
		if r.Type != FieldAmount {
			continue
		}
		if r.Currency == "" {
			add("amount resource %s names no currency dimension: add register:\"resource,currency=<column>\"", r.Column)
		} else if !schema.IsDimension(r.Currency) {
			add("currency %s of resource %s is not a dimension", r.Currency, r.Column)
		}
	}
	if len(problems) > 0 {
		return nil, describe(def, problems)
	}
	return schema, nil
}

func describe(def Definition, problems []string) error {
	return fmt.Errorf("accumulation register %q: %s", def.Key, strings.Join(problems, "; "))
}

// parseRegisterTag splits register:"resource,currency=currency_id" into the
// role and its options.
func parseRegisterTag(tag string) (role string, options map[string]string) {
	parts := strings.Split(tag, ",")
	role = strings.TrimSpace(parts[0])
	for _, part := range parts[1:] {
		name, value, _ := strings.Cut(strings.TrimSpace(part), "=")
		if options == nil {
			options = map[string]string{}
		}
		options[name] = value
	}
	return role, options
}

// metaValue reads key from meta:"label:Склад,ref:warehouse".
func metaValue(field reflect.StructField, key string) string {
	for _, part := range strings.Split(field.Tag.Get("meta"), ",") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(part), key+":"); ok {
			return value
		}
	}
	return ""
}
