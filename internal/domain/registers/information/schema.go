// Package information declares information registers (регистры сведений)
// from their record type, as package accumulation declares accumulation
// registers. A record struct embeds RecordBase and tags its dimensions and
// resources; a Definition names the register, its periodicity and how it is
// written. The platform derives storage, writes, queries (the slice of the
// latest records at a moment) and a check that the tenant database holds the
// table the description promises.
//
//	type Price struct {
//		information.RecordBase
//		ItemID     id.ID            `db:"item_id" meta:"label:Номенклатура,ref:item" register:"dimension"`
//		Price      types.MinorUnits `db:"price" meta:"label:Цена" register:"resource,currency=currency_id"`
//		CurrencyID id.ID            `db:"currency_id" meta:"label:Валюта,ref:currency" register:"resource"`
//	}
//
//	var Prices = information.New[Price](information.Definition{
//		Key: "item_prices", Label: "Цены номенклатуры", Table: "reg_item_prices",
//		Periodicity: information.Day, Mode: information.Recorder,
//	})
package information

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
)

// Periodicity is how a register keeps its records in time.
type Periodicity string

const (
	// NonPeriodic registers keep one record per combination of dimensions.
	NonPeriodic Periodicity = "none"
	// Second registers keep a record per combination and moment, to the
	// second; the record valid at a moment is the latest one before it.
	Second Periodicity = "second"
	// Day registers keep a record per combination and day.
	Day Periodicity = "day"
)

// Mode is how a register is written.
type Mode string

const (
	// Independent registers are written by code: Set and Delete.
	Independent Mode = "independent"
	// Recorder registers are written by posting documents (подчинение
	// регистратору): a document that implements accumulation.Source adds
	// its records with Record, reposting replaces them, unposting removes
	// them.
	Recorder Mode = "recorder"
)

// Definition names a register.
type Definition struct {
	// Key identifies the register; snake case, unique.
	Key string
	// Label is the register's name for people ("Цены номенклатуры").
	Label string
	// Table is the register's table; it starts with "reg_".
	Table string
	// Periodicity is NonPeriodic, Second or Day.
	Periodicity Periodicity
	// Mode is Independent or Recorder.
	Mode Mode
}

// RecordBase holds the fields a record has besides its dimensions and
// resources: its period, for a periodic register, and the document that
// wrote it, for a Recorder register.
type RecordBase struct {
	Period       time.Time `db:"period" json:"period"`
	RecorderID   id.ID     `db:"recorder_id" json:"recorderId"`
	RecorderType string    `db:"recorder_type" json:"recorderType"`
}

// Header returns the record's common fields.
func (b *RecordBase) Header() *RecordBase { return b }

// Record is a pointer to a register's record: its type embeds RecordBase.
type Record interface {
	Header() *RecordBase
}

// FieldType is how a dimension or resource is stored and shown.
type FieldType string

const (
	// FieldRef is a reference (id.ID, uuid).
	FieldRef FieldType = "ref"
	// FieldText is a string (text).
	FieldText FieldType = "text"
	// FieldQuantity is a types.Quantity (bigint).
	FieldQuantity FieldType = "quantity"
	// FieldAmount is a types.MinorUnits in the currency another field names.
	FieldAmount FieldType = "amount"
	// FieldFlag is a bool (boolean).
	FieldFlag FieldType = "flag"
)

// SQLType is the PostgreSQL type of the field's column.
func (t FieldType) SQLType() string {
	switch t {
	case FieldRef:
		return "uuid"
	case FieldText:
		return "text"
	case FieldFlag:
		return "boolean"
	default:
		return "bigint"
	}
}

// Field is a dimension or resource of a register.
type Field struct {
	Column   string
	Label    string
	Type     FieldType
	Ref      string
	Currency string
	index    []int
}

// Schema is a register's definition with the fields of its record type.
type Schema struct {
	Definition
	Dimensions []Field
	Resources  []Field
}

// Periodic reports whether the register keeps its records in time.
func (s *Schema) Periodic() bool { return s.Periodicity != NonPeriodic }

// HeaderColumns are the columns of RecordBase the register stores.
func (s *Schema) HeaderColumns() []string {
	var columns []string
	if s.Periodic() {
		columns = append(columns, "period")
	}
	if s.Mode == Recorder {
		columns = append(columns, "recorder_id", "recorder_type")
	}
	return columns
}

// Columns are the table's columns in the order Values returns them.
func (s *Schema) Columns() []string {
	return append(s.HeaderColumns(), append(s.DimensionColumns(), s.ResourceColumns()...)...)
}

// KeyColumns identify a record: the period and the dimensions.
func (s *Schema) KeyColumns() []string {
	var key []string
	if s.Periodic() {
		key = append(key, "period")
	}
	return append(key, s.DimensionColumns()...)
}

// DimensionColumns are the dimensions' columns.
func (s *Schema) DimensionColumns() []string { return columnsOf(s.Dimensions) }

// ResourceColumns are the resources' columns.
func (s *Schema) ResourceColumns() []string { return columnsOf(s.Resources) }

// IsDimension reports whether column is a dimension of the register.
func (s *Schema) IsDimension(column string) bool {
	return slices.ContainsFunc(s.Dimensions, func(f Field) bool { return f.Column == column })
}

func columnsOf(fields []Field) []string {
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = f.Column
	}
	return columns
}

// Values flattens record into the values of Columns.
func (s *Schema) Values(record Record) []any {
	v := reflect.ValueOf(record).Elem()
	h := record.Header()
	var values []any
	if s.Periodic() {
		values = append(values, h.Period)
	}
	if s.Mode == Recorder {
		values = append(values, h.RecorderID, h.RecorderType)
	}
	for _, f := range s.Dimensions {
		values = append(values, v.FieldByIndex(f.index).Interface())
	}
	for _, f := range s.Resources {
		values = append(values, v.FieldByIndex(f.index).Interface())
	}
	return values
}

// KeyValues are the values of KeyColumns of record.
func (s *Schema) KeyValues(record Record) []any {
	v := reflect.ValueOf(record).Elem()
	var values []any
	if s.Periodic() {
		values = append(values, record.Header().Period)
	}
	for _, f := range s.Dimensions {
		values = append(values, v.FieldByIndex(f.index).Interface())
	}
	return values
}

// PeriodOf is t as the register keeps it: to the day in UTC for a Day
// register, to the second for a Second one.
func (s *Schema) PeriodOf(t time.Time) time.Time {
	switch s.Periodicity {
	case Day:
		u := t.UTC()
		return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	case Second:
		return t.UTC().Truncate(time.Second)
	default:
		return time.Time{}
	}
}

var (
	keyPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	tablePattern   = regexp.MustCompile(`^reg_[a-z0-9_]+$`)
	columnPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	idType         = reflect.TypeFor[id.ID]()
	quantityType   = reflect.TypeFor[types.Quantity]()
	minorUnitsType = reflect.TypeFor[types.MinorUnits]()
	baseType       = reflect.TypeFor[RecordBase]()
	headerNames    = []string{"period", "recorder_id", "recorder_type"}
)

// NewSchema describes the register def whose records have type record. It
// reports every mistake of the description at once.
func NewSchema(def Definition, record reflect.Type) (*Schema, error) {
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
	if def.Periodicity != NonPeriodic && def.Periodicity != Second && def.Periodicity != Day {
		add("periodicity %q is none of %q, %q, %q", def.Periodicity, NonPeriodic, Second, Day)
	}
	if def.Mode != Independent && def.Mode != Recorder {
		add("mode %q is neither %q nor %q", def.Mode, Independent, Recorder)
	}
	schema := &Schema{Definition: def}
	if record.Kind() != reflect.Struct {
		add("record type %s is not a struct", record)
		return nil, describe(def, problems)
	}
	embedsBase := false
	seen := map[string]bool{}
	for _, c := range headerNames {
		seen[c] = true
	}
	for i := range record.NumField() {
		field := record.Field(i)
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
		if !field.IsExported() || !columnPattern.MatchString(column) {
			add("field %s is not exported or has no db column", field.Name)
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
		switch {
		case field.Type == idType:
			f.Type = FieldRef
			if f.Ref == "" {
				add("field %s refers to nothing: add meta:\"ref:<entity key>\"", field.Name)
			}
		case field.Type == quantityType:
			f.Type = FieldQuantity
		case field.Type == minorUnitsType:
			f.Type = FieldAmount
		case field.Type.Kind() == reflect.String:
			f.Type = FieldText
		case field.Type.Kind() == reflect.Bool:
			f.Type = FieldFlag
		default:
			add("field %s has type %s; a field is an id.ID, string, bool, types.Quantity or types.MinorUnits", field.Name, field.Type)
		}
		for name, value := range options {
			if name != "currency" || f.Type != FieldAmount {
				add("field %s: option %s is not known", field.Name, name)
				continue
			}
			f.Currency = value
		}
		switch role {
		case "dimension":
			if f.Type == FieldAmount || f.Type == FieldQuantity || f.Type == FieldFlag {
				add("dimension %s has type %s; a dimension is an id.ID or a string", field.Name, field.Type)
			}
			schema.Dimensions = append(schema.Dimensions, f)
		case "resource":
			schema.Resources = append(schema.Resources, f)
		default:
			add("field %s has register tag %q; it is dimension or resource", field.Name, role)
		}
	}
	if !embedsBase {
		add("record type %s does not embed information.RecordBase", record)
	}
	if len(schema.Resources) == 0 {
		add("the register has no resource")
	}
	if len(schema.Dimensions) == 0 && !schema.Periodic() {
		add("a non-periodic register needs a dimension")
	}
	for _, r := range schema.Resources {
		if r.Type != FieldAmount {
			continue
		}
		currency := slices.IndexFunc(append(slices.Clone(schema.Dimensions), schema.Resources...), func(f Field) bool { return f.Column == r.Currency })
		if r.Currency == "" || currency < 0 {
			add("amount %s names no currency field: add register:\"resource,currency=<column>\"", r.Column)
		}
	}
	if len(problems) > 0 {
		return nil, describe(def, problems)
	}
	return schema, nil
}

func describe(def Definition, problems []string) error {
	return fmt.Errorf("information register %q: %s", def.Key, strings.Join(problems, "; "))
}

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

func metaValue(field reflect.StructField, key string) string {
	for part := range strings.SplitSeq(field.Tag.Get("meta"), ",") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(part), key+":"); ok {
			return value
		}
	}
	return ""
}
