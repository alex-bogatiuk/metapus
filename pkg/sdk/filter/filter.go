// Package filter is the list filter of the platform's list views.
package filter

import (
	core "github.com/alex-bogatiuk/metapus/internal/domain/filter"
)

type (
	// Item represents a single filter condition.
	Item = core.Item
)

const (
	Contains = core.Contains
	Greater  = core.Greater
)

// ExtractSearchQuery removes the __search pseudo-item from a filter slice,
// returning the search query and the remaining "real" items.
func ExtractSearchQuery(items []core.Item) (string, []core.Item) {
	return core.ExtractSearchQuery(items)
}

func Resolve(item core.Item, p1 core.FieldSpec) (core.ResolvedItem, error) {
	return core.Resolve(item, p1)
}
