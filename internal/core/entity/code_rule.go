package entity

import (
	"fmt"
	"regexp"
	"strings"
)

// CodeSource says where the Code of a catalog item comes from.
type CodeSource string

const (
	// CodeFromSeries is the default: when an item is written with an empty
	// code, the platform takes the next code of the catalog's series in the
	// same transaction. A code entered by the user is kept.
	CodeFromSeries CodeSource = "series"
	// CodeDerived means the catalog's own hooks derive the code from its
	// attributes (an ISO code, a ticker); the platform only requires the
	// result to be non-empty.
	CodeDerived CodeSource = "derived"
	// CodeManual means the user enters the code; the platform requires it.
	CodeManual CodeSource = "manual"
)

// DefaultCodeWidth is the minimum number of digits of a series code.
const DefaultCodeWidth = 6

// CodeRule declares how the platform assigns the Code of a catalog item. The
// zero value is a series of the catalog with a prefix derived from its key.
type CodeRule struct {
	Source CodeSource
	// Prefix starts every code of the series: "ORG" gives "ORG-000001". It
	// belongs to the format only; the counter is keyed by the catalog, so a
	// new prefix continues the same sequence.
	Prefix string
	// Width is the minimum number of digits, DefaultCodeWidth when zero.
	Width int
}

// CodeSeries is the resolved series a catalog takes its codes from.
type CodeSeries struct {
	// Key identifies the counter in the tenant database. It is derived from
	// the entity key and never from the displayed prefix.
	Key    string
	Prefix string
	Width  int
}

// CodeSource returns the declared source of the catalog's codes.
func (d Definition) CodeSource() CodeSource {
	if d.Code.Source == "" {
		return CodeFromSeries
	}
	return d.Code.Source
}

// CodeSeries returns the series of a catalog whose codes come from one.
func (d Definition) CodeSeries() CodeSeries {
	series := CodeSeries{Key: "catalog." + d.Key, Prefix: d.Code.Prefix, Width: d.Code.Width}
	if series.Prefix == "" {
		series.Prefix = DefaultCodePrefix(d.Key)
	}
	if series.Width == 0 {
		series.Width = DefaultCodeWidth
	}
	return series
}

// DefaultCodePrefix derives a prefix from an entity key: the first letters of
// its words, e.g. "fuel_card" → "FC", or the first two letters of a single
// word, "vehicle" → "VE".
func DefaultCodePrefix(key string) string {
	words := strings.Split(key, "_")
	if len(words) == 1 {
		return strings.ToUpper(key[:min(2, len(key))])
	}
	var prefix strings.Builder
	for _, w := range words[:min(3, len(words))] {
		if w != "" {
			prefix.WriteByte(w[0])
		}
	}
	return strings.ToUpper(prefix.String())
}

var codePrefix = regexp.MustCompile(`^[\p{L}\p{N}]{1,10}$`)

func (d Definition) validateCode() error {
	if d.Code == (CodeRule{}) {
		return nil
	}
	if d.Kind != CatalogKind {
		return fmt.Errorf("entity %q: only a catalog declares a code rule", d.Name)
	}
	switch d.CodeSource() {
	case CodeFromSeries:
	case CodeDerived, CodeManual:
		if d.Code.Prefix != "" || d.Code.Width != 0 {
			return fmt.Errorf("entity %q: a %s code has no series format", d.Name, d.Code.Source)
		}
		return nil
	default:
		return fmt.Errorf("entity %q: unknown code source %q", d.Name, d.Code.Source)
	}
	if d.Code.Prefix != "" && !codePrefix.MatchString(d.Code.Prefix) {
		return fmt.Errorf("entity %q: code prefix %q must be 1-10 letters or digits", d.Name, d.Code.Prefix)
	}
	if d.Code.Width < 0 || d.Code.Width > 12 {
		return fmt.Errorf("entity %q: code width %d is outside 1..12", d.Name, d.Code.Width)
	}
	return nil
}
