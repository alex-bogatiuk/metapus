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
	quantityType     = reflect.TypeFor[types.Quantity]()
	minorUnitsType   = reflect.TypeFor[types.MinorUnits]()
	cryptoAmountType = reflect.TypeFor[types.CryptoAmount]()
	typedRefType     = reflect.TypeFor[entity.TypedRef]()
	decimalType      = reflect.TypeFor[decimal.Decimal]()
	paletteColorType = reflect.TypeFor[types.PaletteColor]()
)

// Inspect analyzes a struct and returns its EntityDef.
func Inspect(entity any, name string, entityType EntityType) EntityDef {
	t := reflect.TypeOf(entity)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if name == "" {
		name = t.Name()
	}

	def := EntityDef{
		ReadModel:     entity,
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
	for field := range t.Fields() {
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
			DBColumn:   dbColumnName(field),
			Nullable:   field.Type.Kind() == reflect.Pointer,
			Name:       jsonName(field),
			Label:      label,
			Required:   isRequired(field),
			ReadOnly:   isReadOnly(field),
			WriteOnly:  metaHasWriteOnlyTrue(field),
			ListHidden: metaHasListFalse(field),
		}

		// Type mapping
		mapFieldType(&fDef, field)

		// Filter out ignored fields (json:"-") unless needed
		if fDef.Name == "-" {
			continue
		}

		def.Fields = append(def.Fields, fDef)

		// Auto-collect preview fields (top-level only, not table parts).
		if topLevel && !previewSkipFields[fDef.Name] && !metaHasPreviewFalse(field) {
			dbCol := dbColumnName(field)
			if dbCol == "" {
				dbCol = toSnakeCase(field.Name)
			}

			// Case 1: Reference fields (id.ID) on documents — auto-collected.
			if def.Type == TypeDocument && fDef.Type == TypeReference && fDef.ReferenceType != "" {
				def.PreviewFields = append(def.PreviewFields, PreviewFieldDef{
					Name:          fDef.Name,
					Label:         label,
					Column:        dbCol,
					ReferenceType: fDef.ReferenceType,
				})
			}

			// Case 2: Scalar fields with explicit meta:"preview:true" — any entity type.
			if fDef.Type != TypeReference && metaHasPreviewTrue(field) {
				def.PreviewFields = append(def.PreviewFields, PreviewFieldDef{
					Name:   fDef.Name,
					Label:  label,
					Column: dbCol,
					// ReferenceType empty → scalar field, read directly from table
				})
			}
		}
	}
}

func inspectColumns(t reflect.Type) []FieldDef {
	cols := make([]FieldDef, 0)
	for field := range t.Fields() {
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
			Name:       jsonName(field),
			Label:      label,
			Required:   isRequired(field),
			ListHidden: metaHasListFalse(field),
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
	for part := range strings.SplitSeq(tag, ",") {
		part = strings.TrimSpace(part)
		if after, ok0 := strings.CutPrefix(part, "label:"); ok0 {
			return after
		}
	}
	return ""
}

// metaRefType extracts the reference entity key from the "meta" struct tag.
// Supports format: meta:"ref:merchant" or meta:"label:Мерчант,ref:merchant".
// Returns "" if no meta tag or no ref: prefix found.
func metaRefType(field reflect.StructField) string {
	tag, ok := field.Tag.Lookup("meta")
	if !ok {
		return ""
	}
	for part := range strings.SplitSeq(tag, ",") {
		part = strings.TrimSpace(part)
		if after, ok0 := strings.CutPrefix(part, "ref:"); ok0 {
			return after
		}
	}
	return ""
}

// metaFieldType extracts explicit field type from "meta" struct tag, e.g. meta:"type:integer".
func metaFieldType(field reflect.StructField) FieldType {
	tag, ok := field.Tag.Lookup("meta")
	if !ok {
		return ""
	}
	for part := range strings.SplitSeq(tag, ",") {
		part = strings.TrimSpace(part)
		if after, ok0 := strings.CutPrefix(part, "type:"); ok0 {
			return FieldType(after)
		}
	}
	return ""
}

func mapFieldType(def *FieldDef, field reflect.StructField) {
	if explicit := metaFieldType(field); explicit != "" {
		def.Type = explicit
		return
	}

	t := field.Type

	// Dereference pointer types so *id.ID and *time.Time are handled correctly.
	actual := t
	if actual.Kind() == reflect.Pointer {
		actual = actual.Elem()
	}

	// Handle ID -> Reference (both id.ID and *id.ID)
	if actual == reflect.TypeFor[id.ID]() {
		def.Type = TypeReference
		// First: check explicit meta:"ref:xxx" tag
		if refKey := metaRefType(field); refKey != "" {
			def.ReferenceType = refKey
		} else if before, ok := strings.CutSuffix(field.Name, "ID"); ok {
			// Fallback heuristic: e.g. "MerchantID" -> "merchant"
			baseName := before
			def.ReferenceType = strings.ToLower(baseName)
		}
		return
	}

	if actual == reflect.TypeFor[time.Time]() {
		def.Type = TypeDate
		return
	}
	if actual.Kind() == reflect.Map {
		def.Type = TypeJSON
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
	if actual == cryptoAmountType {
		// CryptoAmount is stored in token minor units (sun/wei) — no currency scaling.
		// Use TypeInteger for simple numeric comparison (no currency_id lookup).
		def.Type = TypeInteger
		return
	}

	// A palette colour is an enum the form shows as a colour picker.
	if actual == paletteColorType {
		def.Type = TypeEnum
		def.Widget = WidgetColor
		def.EnumValues = make([]EnumValue, 0, len(types.PaletteChoices))
		for _, choice := range types.PaletteChoices {
			def.EnumValues = append(def.EnumValues, EnumValue{Value: string(choice.Color), Label: choice.Label})
		}
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
	if name := metaName(field); name != "" {
		return name
	}
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

// metaName lets metadata expose a form-only field while the domain struct keeps
// json:"-". This is used together with writeOnly:true for values that may be
// accepted by DTOs but must never be serialized from the stored entity.
func metaName(field reflect.StructField) string {
	tag, ok := field.Tag.Lookup("meta")
	if !ok {
		return ""
	}
	for part := range strings.SplitSeq(tag, ",") {
		part = strings.TrimSpace(part)
		if value, found := strings.CutPrefix(part, "name:"); found {
			return value
		}
	}
	return ""
}

func isRequired(field reflect.StructField) bool {
	// Check validation tags if available, e.g. `binding:"required"`
	if tag, ok := field.Tag.Lookup("binding"); ok {
		return strings.Contains(tag, "required")
	}
	return false
}

func isReadOnly(field reflect.StructField) bool {
	for part := range strings.SplitSeq(field.Tag.Get("meta"), ",") {
		if strings.TrimSpace(part) == "readOnly:true" {
			return true
		}
	}
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

// metaHasListFalse checks if field has meta:"list:false" to opt out of list columns.
func metaHasListFalse(field reflect.StructField) bool {
	tag, ok := field.Tag.Lookup("meta")
	if !ok {
		return false
	}
	for part := range strings.SplitSeq(tag, ",") {
		if strings.TrimSpace(part) == "list:false" {
			return true
		}
	}
	return false
}

func metaHasWriteOnlyTrue(field reflect.StructField) bool {
	tag, ok := field.Tag.Lookup("meta")
	if !ok {
		return false
	}
	for part := range strings.SplitSeq(tag, ",") {
		if strings.TrimSpace(part) == "writeOnly:true" {
			return true
		}
	}
	return false
}

// metaHasPreviewFalse checks if field has meta:"preview:false" to opt out of auto-preview.
func metaHasPreviewFalse(field reflect.StructField) bool {
	tag, ok := field.Tag.Lookup("meta")
	if !ok {
		return false
	}
	for part := range strings.SplitSeq(tag, ",") {
		if strings.TrimSpace(part) == "preview:false" {
			return true
		}
	}
	return false
}

// metaHasPreviewTrue checks if field has meta:"preview:true" to opt in for preview cards.
// Used for scalar fields (INN, phone, email) that should appear in preview.
func metaHasPreviewTrue(field reflect.StructField) bool {
	tag, ok := field.Tag.Lookup("meta")
	if !ok {
		return false
	}
	for part := range strings.SplitSeq(tag, ",") {
		if strings.TrimSpace(part) == "preview:true" {
			return true
		}
	}
	return false
}

// toColumnName converts a Go struct field name to snake_case column name.
// E.g. "CounterpartyID" → "counterparty_id", "MerchantID" → "merchant_id".
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
