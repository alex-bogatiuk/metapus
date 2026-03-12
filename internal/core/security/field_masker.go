package security

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"metapus/internal/core/apperror"
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
	for t.Kind() == reflect.Ptr {
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
	for t.Kind() == reflect.Ptr {
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
			if embedded.Kind() == reflect.Ptr {
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
			if elemType.Kind() == reflect.Ptr {
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
			if embedded.Kind() == reflect.Ptr {
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

// FieldMasker provides field-level security operations.
// It uses cached reflection to efficiently mask fields on read
// and validate field changes on write.
type FieldMasker struct{}

// NewFieldMasker creates a new FieldMasker.
func NewFieldMasker() *FieldMasker {
	return &FieldMasker{}
}

// ValidateWrite compares oldEntity and newEntity, checking if any changed fields
// violate the given FieldPolicy. Returns apperror.Forbidden if a restricted field was modified.
//
// Both entities must be the same type (or pointers to the same type).
// Comparison is done via fmt.Sprintf("%v") for simplicity — sufficient for
// primitive types (string, int, ID, bool) used in document headers.
func (m *FieldMasker) ValidateWrite(oldEntity, newEntity any, policy *FieldPolicy) error {
	if policy == nil {
		return nil // no policy = no restrictions
	}

	oldFields := entityToMap(oldEntity)
	newFields := entityToMap(newEntity)

	for fieldName, newVal := range newFields {
		oldVal, hadOld := oldFields[fieldName]

		// If field didn't change, skip
		if hadOld && fmt.Sprintf("%v", oldVal) == fmt.Sprintf("%v", newVal) {
			continue
		}

		// Field changed — check if policy allows it
		if !policy.IsFieldAllowed(fieldName) {
			return apperror.NewForbidden(
				fmt.Sprintf("field '%s' is read-only", fieldName),
			).WithDetail("field", fieldName)
		}
	}

	return nil
}

// MaskForRead zeroes out fields that the policy doesn't allow reading.
// Modifies the entity in place.
// Also recurses into slice-of-struct fields (table parts) if the policy
// has TableParts restrictions.
func (m *FieldMasker) MaskForRead(entity any, policy *FieldPolicy) {
	if policy == nil {
		return
	}

	v := reflect.ValueOf(entity)
	if v.Kind() == reflect.Ptr {
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
				field.Set(reflect.Zero(field.Type()))
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
			if elem.Kind() == reflect.Ptr {
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
						field.Set(reflect.Zero(field.Type()))
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
	if v.Kind() == reflect.Ptr {
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
