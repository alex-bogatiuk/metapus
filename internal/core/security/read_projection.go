package security

import "reflect"

// ProjectForRead isolates presentation mutations from a domain entity, including
// its pointers, attributes and table parts. Internal immutable scalar types keep
// their concrete representation; no JSON round trip changes their codecs.
func ProjectForRead[T any](source T, policy *FieldPolicy) T {
	value := cloneReadValue(reflect.ValueOf(source))
	if !value.IsValid() {
		return source
	}
	projected := value.Interface().(T)
	if policy != nil {
		MaskForRead(projected, policy)
	}
	return projected
}

func cloneReadValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.New(value.Type().Elem())
		result.Elem().Set(cloneReadValue(value.Elem()))
		return result
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.New(value.Type()).Elem()
		result.Set(cloneReadValue(value.Elem()))
		return result
	case reflect.Struct:
		result := reflect.New(value.Type()).Elem()
		result.Set(value)
		for i := 0; i < value.NumField(); i++ {
			if value.Type().Field(i).IsExported() {
				result.Field(i).Set(cloneReadValue(value.Field(i)))
			}
		}
		return result
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			result.Index(i).Set(cloneReadValue(value.Index(i)))
		}
		return result
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			result.SetMapIndex(iterator.Key(), cloneReadValue(iterator.Value()))
		}
		return result
	default:
		return value
	}
}
