package security

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
)

// fieldMeta holds cached reflection information for a single struct field.
type fieldMeta struct {
	Index    []int  // field index path (supports embedded structs via FieldByIndex)
	JSONName string // json tag name (for matching DTO fields)
	DBName   string // db tag name (for matching policy field names)
}

// tablePartMeta holds cached information about a slice-of-struct field (table part).
type tablePartMeta struct {
	Index    []int        // field index path to the slice field
	PartName string       // table part name (from db or json tag)
	ElemType reflect.Type // element type of the slice (dereferenced if pointer)
}

// entityFieldCache caches struct field metadata per entity type.
// Populated once per entity type at first use.
type entityFieldCache struct {
	mu         sync.RWMutex
	cache      map[reflect.Type][]fieldMeta
	partsMu    sync.RWMutex
	partsCache map[reflect.Type][]tablePartMeta
}

var globalFieldCache = &entityFieldCache{
	cache:      make(map[reflect.Type][]fieldMeta),
	partsCache: make(map[reflect.Type][]tablePartMeta),
}

// getFields returns cached field metadata for a struct type.
// On first call for a type, it extracts field info via reflection and caches it.
func (c *entityFieldCache) getFields(t reflect.Type) []fieldMeta {
	// Dereference pointer types
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	c.mu.RLock()
	fields, ok := c.cache[t]
	c.mu.RUnlock()
	if ok {
		return fields
	}

	// Build cache entry
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if fields, ok := c.cache[t]; ok {
		return fields
	}

	fields = extractFieldMeta(t)
	c.cache[t] = fields
	return fields
}

// getTableParts returns cached table part metadata for a struct type.
func (c *entityFieldCache) getTableParts(t reflect.Type) []tablePartMeta {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	c.partsMu.RLock()
	parts, ok := c.partsCache[t]
	c.partsMu.RUnlock()
	if ok {
		return parts
	}

	c.partsMu.Lock()
	defer c.partsMu.Unlock()

	if parts, ok := c.partsCache[t]; ok {
		return parts
	}

	parts = extractTablePartMeta(t, nil)
	c.partsCache[t] = parts
	return parts
}

// extractTablePartMeta finds slice-of-struct fields in a struct type.
func extractTablePartMeta(t reflect.Type, prefix []int) []tablePartMeta {
	var result []tablePartMeta

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		idxPath := append(append([]int{}, prefix...), i)

		// Recurse into embedded structs
		if f.Anonymous {
			embedded := f.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				result = append(result, extractTablePartMeta(embedded, idxPath)...)
				continue
			}
		}

		// Detect slice-of-struct fields
		if f.Type.Kind() == reflect.Slice {
			elemType := f.Type.Elem()
			if elemType.Kind() == reflect.Pointer {
				elemType = elemType.Elem()
			}
			if elemType.Kind() == reflect.Struct {
				partName := ""
				if dbTag := f.Tag.Get("db"); dbTag != "" && dbTag != "-" {
					partName = strings.SplitN(dbTag, ",", 2)[0]
				}
				if partName == "" {
					if jsonTag := f.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
						partName = strings.SplitN(jsonTag, ",", 2)[0]
					}
				}
				if partName != "" {
					result = append(result, tablePartMeta{
						Index:    idxPath,
						PartName: partName,
						ElemType: elemType,
					})
				}
			}
		}
	}

	return result
}

// extractFieldMeta extracts field metadata from a struct type.
// Handles embedded structs recursively, building multi-level index paths.
func extractFieldMeta(t reflect.Type) []fieldMeta {
	return extractFieldMetaWithPrefix(t, nil)
}

func extractFieldMetaWithPrefix(t reflect.Type, prefix []int) []fieldMeta {
	var result []fieldMeta

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)

		// Skip unexported fields
		if !f.IsExported() {
			continue
		}

		idxPath := append(append([]int{}, prefix...), i)

		// Handle embedded structs
		if f.Anonymous {
			embedded := f.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				result = append(result, extractFieldMetaWithPrefix(embedded, idxPath)...)
				continue
			}
		}

		jsonTag := f.Tag.Get("json")
		dbTag := f.Tag.Get("db")

		// Skip fields with json:"-" or db:"-"
		if jsonTag == "-" || dbTag == "-" {
			continue
		}

		jsonName := ""
		if jsonTag != "" {
			jsonName = strings.SplitN(jsonTag, ",", 2)[0]
		}
		dbName := ""
		if dbTag != "" {
			dbName = strings.SplitN(dbTag, ",", 2)[0]
		}

		if jsonName == "" && dbName == "" {
			continue
		}

		result = append(result, fieldMeta{
			Index:    idxPath,
			JSONName: jsonName,
			DBName:   dbName,
		})
	}
	return result
}

var _createEnvelopeFields = map[string]struct{}{
	"id":            {},
	"version":       {},
	"created_at":    {},
	"updated_at":    {},
	"_deleted_at":   {},
	"_txid":         {},
	"deletion_mark": {},
	"posted":        {},
	"is_folder":     {},
	"date":          {},
	"created_by":    {},
	"updated_by":    {},
}

var _tablePartEnvelopeFields = map[string]struct{}{
	"line_id":     {},
	"id":          {},
	"line_no":     {},
	"document_id": {},
}

// ValidateCreate checks that a newly constructed entity does not populate any
// header or table-part fields forbidden by a "write" FieldPolicy.
func ValidateCreate(newEntity any, policy *FieldPolicy) error {
	if policy == nil {
		return nil
	}
	v := derefStruct(newEntity)
	if !v.IsValid() {
		return nil
	}

	for _, fm := range globalFieldCache.getFields(v.Type()) {
		fieldName := fm.DBName
		if fieldName == "" {
			fieldName = fm.JSONName
		}
		if fieldName == "" {
			continue
		}
		if _, isEnvelope := _createEnvelopeFields[fieldName]; isEnvelope {
			continue
		}
		fieldVal := v.FieldByIndex(fm.Index)
		if fieldVal.IsZero() {
			continue
		}
		if !policy.IsFieldAllowed(fieldName) {
			return apperror.NewForbidden(
				fmt.Sprintf("field '%s' is read-only", fieldName),
			).WithDetail("field", fieldName)
		}
	}

	return validateTablePartsWrite(nil, newEntity, policy)
}

// ValidateWrite compares oldEntity and newEntity, checking if any changed fields
// (both header fields and tabular sections) violate the given FieldPolicy.
// Returns apperror.Forbidden if a restricted field was modified.
func ValidateWrite(oldEntity, newEntity any, policy *FieldPolicy) error {
	if policy == nil {
		return nil // no policy = no restrictions
	}

	oldFields := entityToMap(oldEntity)
	newFields := entityToMap(newEntity)

	for fieldName, newVal := range newFields {
		oldVal, hadOld := oldFields[fieldName]

		// If field didn't change, skip.
		// DeepEqual handles UUID, int, bool, string without string allocations.
		if hadOld && reflect.DeepEqual(oldVal, newVal) {
			continue
		}

		// Field changed — check if policy allows it
		if !policy.IsFieldAllowed(fieldName) {
			return apperror.NewForbidden(
				fmt.Sprintf("field '%s' is read-only", fieldName),
			).WithDetail("field", fieldName)
		}
	}

	return validateTablePartsWrite(oldEntity, newEntity, policy)
}

func derefStruct(entity any) reflect.Value {
	if entity == nil {
		return reflect.Value{}
	}
	v := reflect.ValueOf(entity)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return v
}

func validateTablePartsWrite(oldEntity, newEntity any, policy *FieldPolicy) error {
	if policy == nil || len(policy.TableParts) == 0 {
		return nil
	}
	newVal := derefStruct(newEntity)
	if !newVal.IsValid() {
		return nil
	}
	oldVal := derefStruct(oldEntity)

	for _, part := range globalFieldCache.getTableParts(newVal.Type()) {
		if _, hasPart := policy.TableParts[part.PartName]; !hasPart {
			continue
		}
		newSlice := newVal.FieldByIndex(part.Index)
		if newSlice.Kind() != reflect.Slice || newSlice.Len() == 0 {
			continue
		}

		elemFields := globalFieldCache.getFields(part.ElemType)
		lines := newLineMatcher(part, elemFields, oldVal, newVal.Type())
		for i := range newSlice.Len() {
			newElem := derefStruct(newSlice.Index(i).Interface())
			if !newElem.IsValid() {
				continue
			}
			oldElem, hasOld := lines.match(i, newElem)

			for _, fm := range elemFields {
				colName := fieldColumn(fm)
				if colName == "" {
					continue
				}
				newField := newElem.FieldByIndex(fm.Index)
				if hasOld {
					oldField := oldElem.FieldByIndex(fm.Index)
					if reflect.DeepEqual(oldField.Interface(), newField.Interface()) {
						continue
					}
				} else {
					if _, isEnv := _tablePartEnvelopeFields[colName]; isEnv || newField.IsZero() {
						continue
					}
				}
				if !policy.IsTablePartFieldAllowed(part.PartName, colName) {
					fullName := part.PartName + "." + colName
					return apperror.NewForbidden(
						fmt.Sprintf("field '%s' is read-only", fullName),
					).WithDetail("field", fullName)
				}
			}
		}
	}
	return nil
}

func fieldColumn(fm fieldMeta) string {
	if fm.DBName != "" {
		return fm.DBName
	}
	return fm.JSONName
}

// lineMatcher pairs each submitted table-part row with the stored row it
// edits: by line key when the stored rows carry one, by position otherwise.
type lineMatcher struct {
	key   *fieldMeta
	byKey map[any]reflect.Value
	old   reflect.Value
}

func newLineMatcher(part tablePartMeta, elemFields []fieldMeta, oldVal reflect.Value, newType reflect.Type) lineMatcher {
	var m lineMatcher
	for i := range elemFields {
		name := fieldColumn(elemFields[i])
		if name == "line_id" || name == "id" || elemFields[i].JSONName == "lineId" {
			m.key = &elemFields[i]
			break
		}
	}
	m.byKey = make(map[any]reflect.Value)
	if !oldVal.IsValid() || oldVal.Type() != newType {
		return m
	}
	m.old = oldVal.FieldByIndex(part.Index)
	if m.key == nil || m.old.Kind() != reflect.Slice {
		return m
	}
	for i := range m.old.Len() {
		elem := derefStruct(m.old.Index(i).Interface())
		if !elem.IsValid() {
			continue
		}
		keyField := elem.FieldByIndex(m.key.Index)
		if !keyField.IsZero() && keyField.Type().Comparable() {
			m.byKey[keyField.Interface()] = elem
		}
	}
	return m
}

// match returns the stored row that submitted row i (newElem) edits.
func (m lineMatcher) match(i int, newElem reflect.Value) (reflect.Value, bool) {
	if m.key != nil {
		keyField := newElem.FieldByIndex(m.key.Index)
		if !keyField.IsZero() && keyField.Type().Comparable() {
			if elem, ok := m.byKey[keyField.Interface()]; ok {
				return elem, true
			}
		}
	}
	if len(m.byKey) == 0 && m.old.IsValid() && m.old.Kind() == reflect.Slice && i < m.old.Len() {
		elem := derefStruct(m.old.Index(i).Interface())
		return elem, elem.IsValid()
	}
	return reflect.Value{}, false
}

// RestoreMaskedFields puts back, into an edited entity, the stored values of
// the fields its editor can neither read nor write. MaskForRead sent those
// fields as zero values, and a client saving the form returns them so: left
// as they come, the save would either fail ValidateWrite or, unchecked, wipe
// the hidden value. Only zero values are restored, so an attempt to set such
// a field to something else still reaches ValidateWrite and is refused.
func RestoreMaskedFields(oldEntity, newEntity any, readPolicy, writePolicy *FieldPolicy) {
	if readPolicy == nil || writePolicy == nil {
		return
	}
	newVal := derefStruct(newEntity)
	oldVal := derefStruct(oldEntity)
	if !newVal.IsValid() || !oldVal.IsValid() || oldVal.Type() != newVal.Type() {
		return
	}

	for _, fm := range globalFieldCache.getFields(newVal.Type()) {
		name := fieldColumn(fm)
		if name == "" || readPolicy.IsFieldAllowed(name) || writePolicy.IsFieldAllowed(name) {
			continue
		}
		restoreZero(newVal.FieldByIndex(fm.Index), oldVal.FieldByIndex(fm.Index))
	}

	for _, part := range globalFieldCache.getTableParts(newVal.Type()) {
		newSlice := newVal.FieldByIndex(part.Index)
		if newSlice.Kind() != reflect.Slice || newSlice.Len() == 0 {
			continue
		}
		elemFields := globalFieldCache.getFields(part.ElemType)
		var hidden []fieldMeta
		for _, fm := range elemFields {
			col := fieldColumn(fm)
			if col != "" && !readPolicy.IsTablePartFieldAllowed(part.PartName, col) &&
				!writePolicy.IsTablePartFieldAllowed(part.PartName, col) {
				hidden = append(hidden, fm)
			}
		}
		if len(hidden) == 0 {
			continue
		}
		lines := newLineMatcher(part, elemFields, oldVal, newVal.Type())
		for i := range newSlice.Len() {
			newElem := newSlice.Index(i)
			for newElem.Kind() == reflect.Pointer && !newElem.IsNil() {
				newElem = newElem.Elem()
			}
			if newElem.Kind() != reflect.Struct {
				continue
			}
			oldElem, ok := lines.match(i, newElem)
			if !ok {
				continue
			}
			for _, fm := range hidden {
				restoreZero(newElem.FieldByIndex(fm.Index), oldElem.FieldByIndex(fm.Index))
			}
		}
	}
}

func restoreZero(dst, src reflect.Value) {
	if dst.CanSet() && dst.IsZero() && dst.Type() == src.Type() {
		dst.Set(src)
	}
}

// ValidateListQueryReadPolicy ensures that sorting and filter push-down on entity
// lists cannot be used as an oracle to probe columns masked by a "read" FieldPolicy.
func ValidateListQueryReadPolicy[T any](policy *FieldPolicy, orderBy, defaultOrderCol, search string, searchCols, filterFields []string) error {
	if policy == nil {
		return nil
	}

	tablePartSet := make(map[string]struct{})
	t := reflect.TypeFor[T]()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct {
		for _, part := range globalFieldCache.getTableParts(t) {
			tablePartSet[part.PartName] = struct{}{}
		}
	}

	checkSearch := func() error {
		for _, col := range searchCols {
			if !policy.IsFieldAllowed(col) {
				return apperror.NewForbidden(
					fmt.Sprintf("searching by restricted field '%s' is not allowed", col),
				).WithDetail("field", col)
			}
		}
		return nil
	}

	if strings.TrimSpace(search) != "" {
		if err := checkSearch(); err != nil {
			return err
		}
	}

	sortCol := strings.TrimSpace(orderBy)
	sortCol = strings.TrimPrefix(strings.TrimPrefix(sortCol, "-"), "+")
	if sortCol != "" && sortCol != "id" && sortCol != defaultOrderCol {
		if !policy.IsFieldAllowed(sortCol) {
			return apperror.NewForbidden(
				fmt.Sprintf("sorting by restricted field '%s' is not allowed", sortCol),
			).WithDetail("field", sortCol)
		}
	}

	for _, rawField := range filterFields {
		if rawField == "__search" {
			if err := checkSearch(); err != nil {
				return err
			}
			continue
		}
		if prefix, subfield, hasDot := strings.Cut(rawField, "."); hasDot {
			if _, isPart := tablePartSet[prefix]; isPart {
				if !policy.IsTablePartFieldAllowed(prefix, subfield) {
					return apperror.NewForbidden(
						fmt.Sprintf("filtering by restricted field '%s' is not allowed", rawField),
					).WithDetail("field", rawField)
				}
				continue
			}
			if !policy.IsFieldAllowed(prefix) {
				return apperror.NewForbidden(
					fmt.Sprintf("filtering by restricted field '%s' is not allowed", prefix),
				).WithDetail("field", prefix)
			}
			continue
		}
		if rawField == "id" {
			continue
		}
		if !policy.IsFieldAllowed(rawField) {
			return apperror.NewForbidden(
				fmt.Sprintf("filtering by restricted field '%s' is not allowed", rawField),
			).WithDetail("field", rawField)
		}
	}
	return nil
}

// MaskForRead zeroes out fields that the policy doesn't allow reading.
// Modifies the entity in place.
// Also recurses into slice-of-struct fields (table parts) if the policy
// has TableParts restrictions.
func MaskForRead(entity any, policy *FieldPolicy) {
	if policy == nil {
		return
	}

	v := reflect.ValueOf(entity)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}

	// Mask header-level fields
	fields := globalFieldCache.getFields(v.Type())
	for _, fm := range fields {
		fieldName := fm.DBName
		if fieldName == "" {
			fieldName = fm.JSONName
		}

		if !policy.IsFieldAllowed(fieldName) {
			field := v.FieldByIndex(fm.Index)
			if field.CanSet() {
				field.SetZero()
			}
		}
	}

	// Mask table part fields (slice-of-struct)
	if policy.TableParts == nil {
		return
	}

	parts := globalFieldCache.getTableParts(v.Type())
	for _, part := range parts {
		if _, hasPart := policy.TableParts[part.PartName]; !hasPart {
			continue // no restrictions for this table part
		}

		sliceVal := v.FieldByIndex(part.Index)
		if sliceVal.Kind() != reflect.Slice {
			continue
		}

		elemFields := globalFieldCache.getFields(part.ElemType)
		for i := 0; i < sliceVal.Len(); i++ {
			elem := sliceVal.Index(i)
			if elem.Kind() == reflect.Pointer {
				elem = elem.Elem()
			}

			for _, fm := range elemFields {
				colName := fm.DBName
				if colName == "" {
					colName = fm.JSONName
				}

				if !policy.IsTablePartFieldAllowed(part.PartName, colName) {
					field := elem.FieldByIndex(fm.Index)
					if field.CanSet() {
						field.SetZero()
					}
				}
			}
		}
	}
}

// entityToMap converts a struct to a map[string]any using db tags as keys.
// Falls back to json tags if db tag is not available.
func entityToMap(entity any) map[string]any {
	v := reflect.ValueOf(entity)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}

	fields := globalFieldCache.getFields(v.Type())
	result := make(map[string]any, len(fields))

	for _, fm := range fields {
		key := fm.DBName
		if key == "" {
			key = fm.JSONName
		}
		if key == "" {
			continue
		}

		field := v.FieldByIndex(fm.Index)
		result[key] = field.Interface()
	}

	return result
}
