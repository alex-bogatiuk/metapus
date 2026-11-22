// Package secretfile reads secrets from files named by environment
// variables.
package secretfile

import (
	core "metapus/internal/platform/secretfile"
)

// Get returns a value or fallback when neither NAME nor NAME_FILE is set, or
// when the selected source contains an empty value.
func Get(name string, fallback string) (string, error) { return core.Get(name, fallback) }
