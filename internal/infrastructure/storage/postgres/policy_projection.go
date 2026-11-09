package postgres

import (
	"fmt"
	"metapus/internal/infrastructure/readpolicy"
	"reflect"
	"strings"

	"metapus/internal/core/id"
)

func (a *authorizedReadAdapter) PolicyProjection(key, alias string) (map[string]readpolicy.SQLPolicyField, error) {
	def, ok := a.model(key)
	if !ok || def.ReadModel == nil {
		return nil, fmt.Errorf("policy model is not registered")
	}
	fields := make(map[string]readpolicy.SQLPolicyField)
	var inspect func(reflect.Type)
	inspect = func(t reflect.Type) {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return
		}
		for f := range t.Fields() {
			if f.PkgPath != "" {
				continue
			}
			if f.Anonymous {
				inspect(f.Type)
				continue
			}
			column := strings.Split(f.Tag.Get("db"), ",")[0]
			if column == "" || column == "-" {
				continue
			}
			valueType := f.Type
			nullable := valueType.Kind() == reflect.Pointer
			for valueType.Kind() == reflect.Pointer {
				valueType = valueType.Elem()
			}
			field := readpolicy.SQLPolicyField{SQL: alias + "." + column, Nullable: nullable}
			if valueType == reflect.TypeFor[id.ID]() {
				field.Kind = readpolicy.SQLString
				field.SQL = "CASE WHEN " + field.SQL + " = '00000000-0000-0000-0000-000000000000'::uuid THEN '' ELSE " + field.SQL + "::text END"
				if !nullable {
					field.SQL = "COALESCE((" + field.SQL + "), '')"
				}
			} else if valueType.Implements(reflect.TypeFor[fmt.Stringer]()) {
				// Quantity, decimal and time have codecs that SQL must not guess.
				continue
			} else {
				switch valueType.Kind() {
				case reflect.String:
					field.Kind = readpolicy.SQLString
				case reflect.Bool:
					field.Kind = readpolicy.SQLBoolean
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					field.Kind = readpolicy.SQLInteger
				default:
					continue
				}
			}
			fields[column] = field
			if name := strings.Split(f.Tag.Get("json"), ",")[0]; name != "" && name != "-" {
				fields[name] = field
			}
		}
	}
	inspect(reflect.TypeOf(def.ReadModel))
	return fields, nil
}
