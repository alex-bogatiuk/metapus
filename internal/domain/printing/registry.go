// Package printing provides the print form engine for Metapus documents.
// Follows the CODE IS METADATA pattern: templates are files in the repository,
// not rows in a database.
package printing

import (
	"slices"
	"sync"
)

// PrintFormCategory distinguishes between built-in and custom print forms.
type PrintFormCategory string

const (
	// CategoryStandard marks a built-in (platform) print form.
	CategoryStandard PrintFormCategory = "standard"
	// CategoryCustom marks a customer-added print form.
	CategoryCustom PrintFormCategory = "custom"
)

// PrintFormDef defines a single print form for a document type.
type PrintFormDef struct {
	// Name is the machine-readable form identifier used in API query param, e.g. "standard".
	Name string
	// Label is the human-readable form name shown in the UI, e.g. "Goods Receipt".
	Label string
	// Template is the .gohtml file name inside the templates/ directory.
	Template string
	// PaperSize is the CSS @page paper size hint, e.g. "A4".
	PaperSize string
	// Category groups forms in the UI (standard = built-in, custom = customer-added).
	Category PrintFormCategory
	// SortOrder controls the display order within a category (lower = first).
	SortOrder int
}

// PrintFormRegistry stores available print forms per document type.
// Follows the same Abstract Factory pattern as catalogFactories / documentFactories.
type PrintFormRegistry struct {
	mu    sync.RWMutex
	forms map[string][]PrintFormDef // docType → []PrintFormDef (ordered, first = default)
}

// NewPrintFormRegistry creates a registry with the built-in standard forms.
func NewPrintFormRegistry() *PrintFormRegistry {
	r := &PrintFormRegistry{
		forms: make(map[string][]PrintFormDef),
	}
	r.Register("goods_receipt", PrintFormDef{
		Name:      "standard",
		Label:     "Поступление товаров",
		Template:  "goods_receipt.gohtml",
		PaperSize: "A4",
		Category:  CategoryStandard,
		SortOrder: 0,
	})
	r.Register("goods_issue", PrintFormDef{
		Name:      "standard",
		Label:     "Реализация товаров",
		Template:  "goods_issue.gohtml",
		PaperSize: "A4",
		Category:  CategoryStandard,
		SortOrder: 0,
	})
	return r
}

// Register adds a print form definition for a document type.
// Documents types are keyed by their snake_case name, e.g. "goods_receipt".
func (r *PrintFormRegistry) Register(docType string, def PrintFormDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forms[docType] = append(r.forms[docType], def)
}

// GetForm returns a specific form by docType + form name.
// If name is empty, returns the first (default) form.
// Returns (def, true) on success, (zero, false) if not found.
func (r *PrintFormRegistry) GetForm(docType, name string) (PrintFormDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	forms := r.forms[docType]
	if len(forms) == 0 {
		return PrintFormDef{}, false
	}
	if name == "" {
		return forms[0], true
	}
	for _, f := range forms {
		if f.Name == name {
			return f, true
		}
	}
	return PrintFormDef{}, false
}

// PrintFormSummary is a lightweight descriptor for the API list endpoint.
type PrintFormSummary struct {
	Name     string            `json:"name"`
	Label    string            `json:"label"`
	Category PrintFormCategory `json:"category"`
}

// ListForms returns summary descriptors for all forms of a document type,
// sorted by SortOrder for stable UI display.
func (r *PrintFormRegistry) ListForms(docType string) []PrintFormSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	forms := r.forms[docType]

	sorted := make([]PrintFormDef, len(forms))
	copy(sorted, forms)
	slices.SortStableFunc(sorted, func(a, b PrintFormDef) int {
		return a.SortOrder - b.SortOrder
	})

	out := make([]PrintFormSummary, len(sorted))
	for i, f := range sorted {
		out[i] = PrintFormSummary{Name: f.Name, Label: f.Label, Category: f.Category}
	}
	return out
}
