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
	Index    int    // field index in struct
	JSONName string // json tag name (for matching DTO fields)
	DBName   string // db tag name (for matching policy field names)
}

// entityFieldCache caches struct field metadata per entity type.
// Populated once per entity type at first use.
type entityFieldCache struct {
	mu     sync.RWMutex
	cache  map[reflect.Type][]fieldMeta
}

var globalFieldCache = &entityFieldCache{
	cache: make(map[reflect.Type][]fieldMeta),
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

// extractFieldMeta extracts field metadata from a struct type.
// Handles embedded structs recursively.
func extractFieldMeta(t reflect.Type) []fieldMeta {
	var result []fieldMeta

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)

		// Skip unexported fields
		if !f.IsExported() {
			continue
		}

		// Handle embedded structs
		if f.Anonymous {
			embedded := f.Type
			if embedded.Kind() == reflect.Ptr {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				result = append(result, extractFieldMeta(embedded)...)
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
			Index:    i,
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

	fields := globalFieldCache.getFields(v.Type())
	for _, fm := range fields {
		fieldName := fm.DBName
		if fieldName == "" {
			fieldName = fm.JSONName
		}

		if !policy.IsFieldAllowed(fieldName) {
			field := v.Field(fm.Index)
			if field.CanSet() {
				field.Set(reflect.Zero(field.Type()))
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

		field := v.Field(fm.Index)
		result[key] = field.Interface()
	}

	return result
}
