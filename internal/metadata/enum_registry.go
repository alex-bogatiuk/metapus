package metadata

import (
	"reflect"
	"sync"
)

// EnumValue represents a single value within an enumeration,
// conveying both its backend identifier and a human-readable label.
type EnumValue struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// EnumDef contains the ordered sequence of allowed enum values.
type EnumDef struct {
	Values []EnumValue
}

var (
	enumRegistryMu sync.RWMutex
	enumRegistry   = make(map[reflect.Type]EnumDef)
)

// RegisterEnum registers a generic, string-based enumeration type.
// The provided slice controls the order in which options render
// in frontend drop-downs and selection menus.
func RegisterEnum[T ~string](values []EnumValue) {
	t := reflect.TypeOf(T(""))
	enumRegistryMu.Lock()
	defer enumRegistryMu.Unlock()

	enumRegistry[t] = EnumDef{
		Values: values,
	}
}

// lookupEnum checks the enum registry for a given reflection type
// and safely returns the enum definition, if any was registered.
func lookupEnum(t reflect.Type) (EnumDef, bool) {
	enumRegistryMu.RLock()
	defer enumRegistryMu.RUnlock()

	def, exists := enumRegistry[t]
	return def, exists
}
