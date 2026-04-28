package metadata

import (
	"reflect"
	"strings"
	"time"
	"unicode"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"

	"github.com/shopspring/decimal"
)

// Reflect types for storage-scaled numeric types and compound references.
var (
	quantityType   = reflect.TypeOf(types.Quantity(0))
	minorUnitsType = reflect.TypeOf(types.MinorUnits(0))
	typedRefType   = reflect.TypeOf(entity.TypedRef{})
	decimalType    = reflect.TypeOf(decimal.Decimal{})
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
		Name:          name,
		Type:          entityType,
		Fields:        make([]FieldDef, 0),
		TableParts:    make([]TablePartDef, 0),
		PreviewFields: make([]PreviewFieldDef, 0),
	}

	inspectStruct(t, &def, true) // topLevel=true for preview field collection

	return def
}

// previewSkipFields are fields that should NOT appear in preview cards.
var previewSkipFields = map[string]bool{
	"id": true, "organizationId": true, "basisId": true, "basisType": true,
	"createdBy": true, "updatedBy": true, "parentId": true,
}

func inspectStruct(t reflect.Type, def *EntityDef, topLevel bool) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if field.PkgPath != "" { // unexported
			continue
		}

		// Handle embedded structs (flattening)
		if field.Anonymous {
			// Special case: TypedRef emits a single compound field
			if field.Type == typedRefType {
				def.Fields = append(def.Fields, FieldDef{
					Name:  "typedRef",
					Label: "Документ-основание",
					Type:  TypeTypedRef,
				})
				continue
			}
			inspectStruct(field.Type, def, topLevel)
			continue
		}

		// Check for TablePart (Slice of Structs)
		if field.Type.Kind() == reflect.Slice {
			elemType := field.Type.Elem()
			if elemType.Kind() == reflect.Struct {
				// Identify as TablePart (Lines)
				partName := jsonName(field)
				label := metaLabel(field)
				if label == "" {
					label = guessLabel(field.Name)
				}
				tp := TablePartDef{
					Name:    partName,
					Label:   label,
					Columns: inspectColumns(elemType),
				}
				def.TableParts = append(def.TableParts, tp)
				continue
			}
		}

		// Regular Field
		label := metaLabel(field)
		if label == "" {
			label = guessLabel(field.Name)
		}
		fDef := FieldDef{
			Name:     jsonName(field),
			Label:    label,
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

		// Auto-collect preview fields: reference fields from document headers
		// (top-level only, not table parts). Skip system fields.
		if topLevel && def.Type == TypeDocument && fDef.Type == TypeReference &&
			fDef.ReferenceType != "" && !previewSkipFields[fDef.Name] &&
			!metaHasPreviewFalse(field) {

			dbCol := dbColumnName(field)
			if dbCol == "" {
				dbCol = toSnakeCase(field.Name)
			}
			def.PreviewFields = append(def.PreviewFields, PreviewFieldDef{
				Name:          fDef.Name,
				Label:         label,
				Column:        dbCol,
				ReferenceType: fDef.ReferenceType,
			})
		}
	}
}


func inspectColumns(t reflect.Type) []FieldDef {
	cols := make([]FieldDef, 0)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}

		// Special case: embedded TypedRef → single compound column
		if field.Anonymous && field.Type == typedRefType {
			cols = append(cols, FieldDef{
				Name:  "typedRef",
				Label: "Документ-основание",
				Type:  TypeTypedRef,
			})
			continue
		}

		label := metaLabel(field)
		if label == "" {
			label = guessLabel(field.Name)
		}
		fDef := FieldDef{
			Name:     jsonName(field),
			Label:    label,
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

// metaLabel extracts a human-readable label from the "meta" struct tag.
// Supports format: meta:"label:Supplier" or meta:"label:Supplier,other:val".
// Returns "" if no meta tag or no label: prefix found.
func metaLabel(field reflect.StructField) string {
	tag, ok := field.Tag.Lookup("meta")
	if !ok {
		return ""
	}
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "label:") {
			return strings.TrimPrefix(part, "label:")
		}
	}
	return ""
}

// metaRefType extracts the reference entity key from the "meta" struct tag.
// Supports format: meta:"ref:vat_rate" or meta:"label:Ставка НДС,ref:vat_rate".
// Returns "" if no meta tag or no ref: prefix found.
func metaRefType(field reflect.StructField) string {
	tag, ok := field.Tag.Lookup("meta")
	if !ok {
		return ""
	}
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "ref:") {
			return strings.TrimPrefix(part, "ref:")
		}
	}
	return ""
}

func mapFieldType(def *FieldDef, field reflect.StructField) {
	t := field.Type

	// Dereference pointer types so *id.ID and *time.Time are handled correctly.
	actual := t
	if actual.Kind() == reflect.Ptr {
		actual = actual.Elem()
	}

	// Handle ID -> Reference (both id.ID and *id.ID)
	if actual == reflect.TypeOf(id.ID{}) {
		def.Type = TypeReference
		// First: check explicit meta:"ref:xxx" tag
		if refKey := metaRefType(field); refKey != "" {
			def.ReferenceType = refKey
		} else if strings.HasSuffix(field.Name, "ID") {
			// Fallback heuristic: e.g. "WarehouseID" -> "warehouse"
			baseName := strings.TrimSuffix(field.Name, "ID")
			def.ReferenceType = strings.ToLower(baseName)
		}
		return
	}

	if actual == reflect.TypeOf(time.Time{}) {
		def.Type = TypeDate
		return
	}

	// Detect storage-scaled numeric types before the Kind() switch.
	if actual == quantityType {
		def.Type = TypeNumber
		def.ValueScale = int(types.QuantityScale) // 10000
		return
	}
	if actual == minorUnitsType {
		def.Type = TypeMoney
		// No static ValueScale — MinorUnits scale depends on the document's currency
		// (cat_currencies.minor_multiplier). Resolved dynamically at SQL level.
		return
	}

	// Dynamic Enum lookup from the metadata registry
	if enumDef, ok := lookupEnum(actual); ok {
		def.Type = TypeEnum
		def.EnumValues = enumDef.Values
		return
	}

	// Handle shopspring/decimal.Decimal → TypeDecimal (number)
	if actual == decimalType {
		def.Type = TypeDecimal
		def.Scale = 4
		return
	}

	switch actual.Kind() {
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

// dbColumnName extracts the DB column name from the "db" struct tag.
// E.g. `db:"counterparty_id"` → "counterparty_id".
func dbColumnName(field reflect.StructField) string {
	if tag, ok := field.Tag.Lookup("db"); ok {
		parts := strings.Split(tag, ",")
		if parts[0] != "" && parts[0] != "-" {
			return parts[0]
		}
	}
	return ""
}

// metaHasPreviewFalse checks if field has meta:"preview:false" to opt out of auto-preview.
func metaHasPreviewFalse(field reflect.StructField) bool {
	tag, ok := field.Tag.Lookup("meta")
	if !ok {
		return false
	}
	for _, part := range strings.Split(tag, ",") {
		if strings.TrimSpace(part) == "preview:false" {
			return true
		}
	}
	return false
}

// toSnakeCase converts CamelCase to snake_case.
// E.g. "CounterpartyID" → "counterparty_id", "WarehouseID" → "warehouse_id".
func toSnakeCase(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}
