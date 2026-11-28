package entity

import "reflect"

// Extensions are the values of an object's extensions: the fields an add-on
// declares for an object of its base or of the platform (registry.Extend).
// Each value is a pointer to the extension's struct, at most one per struct
// type. The values live in the extensions' own tables, one row per object
// (<table of the object>__<add-on>), and are loaded and written with the
// object in its transaction; the object's own table never holds them.
type Extensions []any

// Extensible is an object that carries extension values: every catalog and
// document, through BaseEntity.
type Extensible interface {
	ExtensionValues() *Extensions
}

// ExtensionValues returns the object's extension values.
func (b *BaseEntity) ExtensionValues() *Extensions { return &b.Extensions }

// Find returns the value whose type is t (a pointer to the extension's
// struct), or nil.
func (e Extensions) Find(t reflect.Type) any {
	for _, value := range e {
		if value != nil && reflect.TypeOf(value) == t {
			return value
		}
	}
	return nil
}

// Put sets value (a pointer to an extension's struct), replacing the value of
// the same type.
func (e *Extensions) Put(value any) {
	t := reflect.TypeOf(value)
	for i, existing := range *e {
		if existing != nil && reflect.TypeOf(existing) == t {
			(*e)[i] = value
			return
		}
	}
	*e = append(*e, value)
}

// ExtensionOf returns the object's value of the extension E, adding a zero
// one when the object has none yet: a new object, or one stored before the
// add-on was installed.
func ExtensionOf[E any](object Extensible) *E {
	values := object.ExtensionValues()
	if found, ok := values.Find(reflect.TypeFor[*E]()).(*E); ok {
		return found
	}
	value := new(E)
	values.Put(value)
	return value
}
