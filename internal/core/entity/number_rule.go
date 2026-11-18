package entity

import "fmt"

// NumberRule declares how the platform numbers a document. The zero value
// numbers it from a yearly series whose prefix is derived from its key.
type NumberRule struct {
	// Prefix starts every number of the document: "CW" gives "CW-2026-00001".
	// The series counter is keyed by the prefix and year, so a new prefix
	// starts a new series; its numbers cannot collide with the old ones.
	Prefix string
}

// NumberPrefix returns the prefix of the document's numbers: the declared
// one, or one derived from its key (see DefaultCodePrefix).
func (d Definition) NumberPrefix() string {
	if d.Number.Prefix != "" {
		return d.Number.Prefix
	}
	return DefaultCodePrefix(d.Key)
}

func (d Definition) validateNumber() error {
	if d.Number == (NumberRule{}) {
		return nil
	}
	if d.Kind != DocumentKind {
		return fmt.Errorf("entity %q: only a document declares a number rule", d.Name)
	}
	if !codePrefix.MatchString(d.Number.Prefix) {
		return fmt.Errorf("entity %q: number prefix %q must be 1-10 letters or digits", d.Name, d.Number.Prefix)
	}
	return nil
}
