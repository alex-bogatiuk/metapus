package metadata

import (
	"reflect"
	"strings"
	"time"
	"unicode"

	"metapus/internal/core/id"
)

// Inspect analyzes a struct and returns its EntityDef.
func Inspect(entity interface{}, name string, entityType EntityType) EntityDef {
	t := reflect.TypeOf(entity)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if name == "" {
		name = t.Name()
	}

	def := EntityDef{
		Name:       name,
		Label:      guessLabel(name),
		Type:       entityType,
		Fields:     make([]FieldDef, 0),
		TableParts: make([]TablePartDef, 0),
	}

	inspectStruct(t, &def)

	return def
}

func inspectStruct(t reflect.Type, def *EntityDef) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if field.PkgPath != "" { // unexported
			continue
		}

		// Handle embedded structs (flattening)
		if field.Anonymous {
			inspectStruct(field.Type, def)
			continue
		}

		// Check for TablePart (Slice of Structs)
		if field.Type.Kind() == reflect.Slice {
			elemType := field.Type.Elem()
			if elemType.Kind() == reflect.Struct {
				// Identify as TablePart (Lines)
				partName := jsonName(field)
				tp := TablePartDef{
					Name:    partName,
					Label:   guessLabel(field.Name),
					Columns: inspectColumns(elemType),
				}
				def.TableParts = append(def.TableParts, tp)
				continue
			}
		}

		// Regular Field
		fDef := FieldDef{
			Name:     jsonName(field),
			Label:    guessLabel(field.Name),
			Required: isRequired(field),
			ReadOnly: isReadOnly(field),
		}

		// Type mapping
		mapFieldType(&fDef, field)

		// Filter out ignored fields (json:"-") unless needed
		if fDef.Name == "-" {
			continue
		}

		def.Fields = append(def.Fields, fDef)
	}
}

func inspectColumns(t reflect.Type) []FieldDef {
	cols := make([]FieldDef, 0)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}

		fDef := FieldDef{
			Name:     jsonName(field),
			Label:    guessLabel(field.Name),
			Required: isRequired(field),
		}
		mapFieldType(&fDef, field)
		if fDef.Name == "-" {
			continue
		}
		cols = append(cols, fDef)
	}
	return cols
}

func mapFieldType(def *FieldDef, field reflect.StructField) {
	t := field.Type

	// Handle ID -> Reference
	if t == reflect.TypeOf(id.ID{}) {
		def.Type = TypeReference
		// Guess reference type from name: e.g. "WarehouseID" -> "warehouse"
		name := field.Name
		if strings.HasSuffix(name, "ID") {
			baseName := strings.TrimSuffix(name, "ID")
			def.ReferenceType = strings.ToLower(baseName) // simple heuristic
		}
		return
	}

	if t == reflect.TypeOf(time.Time{}) {
		def.Type = TypeDate
		return
	}

	switch t.Kind() {
	case reflect.String:
		def.Type = TypeString
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Check if it's Money/Amount by name convention
		if strings.Contains(field.Name, "Amount") || strings.Contains(field.Name, "Price") {
			def.Type = TypeMoney
		} else {
			def.Type = TypeInteger
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		def.Type = TypeInteger
	case reflect.Float32, reflect.Float64:
		def.Type = TypeNumber
		def.Scale = 2 // default
		if strings.Contains(field.Name, "Quantity") {
			def.Scale = 3
		}
	case reflect.Bool:
		def.Type = TypeBoolean
	default:
		def.Type = TypeString // fallback
	}
}

func jsonName(field reflect.StructField) string {
	if tag, ok := field.Tag.Lookup("json"); ok {
		parts := strings.Split(tag, ",")
		if parts[0] != "" {
			return parts[0]
		}
	}
	// Fallback: camelCase
	runes := []rune(field.Name)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func isRequired(field reflect.StructField) bool {
	// Check validation tags if available, e.g. `binding:"required"`
	if tag, ok := field.Tag.Lookup("binding"); ok {
		return strings.Contains(tag, "required")
	}
	return false
}

func isReadOnly(field reflect.StructField) bool {
	// Heuristic: ID, Date usually generated
	if field.Name == "ID" || field.Name == "CreatedAt" || field.Name == "UpdatedAt" {
		return true
	}
	return false
}

func guessLabel(name string) string {
	// Simple CamelCase splitter could go here. For now, return name.
	// Ideally we would look up translation map.
	return name
}
