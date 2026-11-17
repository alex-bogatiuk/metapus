package postgres

import (
	"fmt"
	"reflect"
	"time"

	"metapus/internal/core/types"
	"metapus/internal/domain/filter"
)

// ExtractFilterFields derives trusted filter storage semantics from actual Go
// types and db tags. An optional column whitelist restricts the exposed fields.
// It is used once when repositories are registered, never for each SQL row.
func ExtractFilterFields[T any](columns ...string) map[string]filter.FieldSpec {
	return ExtractFilterFieldsOf(reflect.TypeFor[T](), columns...)
}

// ExtractFilterFieldsOf is ExtractFilterFields for a type known at run time,
// such as the line type of a table part.
func ExtractFilterFieldsOf(model reflect.Type, columns ...string) map[string]filter.FieldSpec {
	fields := make(map[string]filter.FieldSpec)
	var inspect func(reflect.Type)
	inspect = func(model reflect.Type) {
		for model.Kind() == reflect.Pointer {
			model = model.Elem()
		}
		if model.Kind() != reflect.Struct {
			return
		}
		for field := range model.Fields() {
			if field.Anonymous {
				inspect(field.Type)
				continue
			}
			column := field.Tag.Get("db")
			if column == "" || column == "-" {
				continue
			}
			fieldType := field.Type
			for fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			kind := filter.NativeValue
			switch fieldType {
			case reflect.TypeFor[types.MinorUnits]():
				kind = filter.MoneyValue
			case reflect.TypeFor[types.Quantity]():
				kind = filter.QuantityValue
			case reflect.TypeFor[time.Time]():
				kind = filter.DateValue
			}
			fields[column] = filter.FieldSpec{Kind: kind}
		}
	}
	inspect(model)
	if len(columns) == 0 {
		return fields
	}
	selected := make(map[string]filter.FieldSpec, len(columns))
	for _, column := range columns {
		spec, ok := fields[column]
		if !ok {
			panic(fmt.Sprintf("filter column %q is not declared by %v", column, model))
		}
		selected[column] = spec
	}
	return selected
}
