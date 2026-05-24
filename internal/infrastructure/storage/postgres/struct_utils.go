package postgres

import (
	"reflect"
	"sync"
)

// ExtractDBColumns extracts all column names from struct "db" tags.
// It handles embedded structs (like entity.Catalog) recursively.
// This function is called once at initialization time, so reflection overhead is acceptable.
//
// Usage:
//
//	columns := ExtractDBColumns[currency.Currency]()
//	// Returns: ["id", "code", "name", "iso_code", "symbol", ...]
func ExtractDBColumns[T any]() []string {
	var zero T
	t := reflect.TypeOf(zero)
	return extractColumnsFromType(t)
}

// extractColumnsFromType recursively extracts column names from a type.
func extractColumnsFromType(t reflect.Type) []string {
	// Dereference pointer types
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil
	}

	var cols []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Handle embedded structs (e.g., entity.Catalog, entity.BaseCatalog)
		if field.Anonymous {
			embeddedCols := extractColumnsFromType(field.Type)
			cols = append(cols, embeddedCols...)
			continue
		}

		// Get db tag
		tag := field.Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}

		// Add column name
		cols = append(cols, tag)
	}

	return cols
}

// flatFieldInfo contains a pre-computed accessor for a struct field,
// including fields from embedded structs. The index path is a chain of
// field indices from outermost to innermost struct, compatible with
// reflect.Value.FieldByIndex for direct access without recursion.
type flatFieldInfo struct {
	indexPath []int  // e.g. [0, 2] means Struct.Field(0).Field(2)
	dbTag     string // Database column name from "db" struct tag
}

// flatTypeMetadata contains the fully flattened field list for a struct type,
// computed once and cached for all subsequent StructToMap calls.
type flatTypeMetadata struct {
	fields []flatFieldInfo
}

// Global cache for flattened type metadata (thread-safe).
var (
	flatTypeCache sync.Map // map[reflect.Type]*flatTypeMetadata
)

// getOrCreateFlatMetadata returns cached flat metadata or creates it.
// First call for a type does full reflection walk (including embedded structs).
// All subsequent calls return the cached flat list — O(1) lookup + O(N) field access.
func getOrCreateFlatMetadata(t reflect.Type) *flatTypeMetadata {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if cached, ok := flatTypeCache.Load(t); ok {
		return cached.(*flatTypeMetadata)
	}

	meta := &flatTypeMetadata{}
	if t.Kind() == reflect.Struct {
		meta.fields = flattenFields(t, nil)
	}

	flatTypeCache.Store(t, meta)
	return meta
}

// flattenFields recursively builds a flat list of (indexPath, dbTag) for all
// "db"-tagged fields, resolving embedded structs at build time.
func flattenFields(t reflect.Type, parentPath []int) []flatFieldInfo {
	var result []flatFieldInfo

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		currentPath := append(append([]int(nil), parentPath...), i)

		if field.Anonymous {
			ft := field.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				result = append(result, flattenFields(ft, currentPath)...)
			}
			continue
		}

		tag := field.Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}

		result = append(result, flatFieldInfo{
			indexPath: currentPath,
			dbTag:     tag,
		})
	}

	return result
}

// StructToMap converts a struct to a map using "db" tags.
// It only includes fields that have a "db" tag and are not ignored ("-").
//
// Performance: Uses cached FLAT type metadata — embedded structs are resolved
// at cache-build time, not at every call. The hot path is a single loop over
// pre-computed index paths with direct FieldByIndex access, eliminating:
//   - recursive StructToMap calls for embedded structs
//   - intermediate map allocations and merging
//
// First call for a type does full reflection walk. All subsequent calls
// use the cached flat field list — the only per-call reflection is
// FieldByIndex().Interface() for each mapped field.
func StructToMap(v any) map[string]any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return nil
	}

	meta := getOrCreateFlatMetadata(rv.Type())

	// Pre-allocate result map with exact capacity
	res := make(map[string]any, len(meta.fields))

	// Single flat loop — no recursion, no intermediate maps
	for _, fi := range meta.fields {
		res[fi.dbTag] = rv.FieldByIndex(fi.indexPath).Interface()
	}

	return res
}
