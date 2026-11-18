package metadata

import "metapus/internal/core/entity"

// CodeAssignment tells forms how the server fills the code of a catalog
// item, as entity.Definition.Code declares it.
type CodeAssignment struct {
	// Source is "series" (the server numbers an empty code when the item is
	// written), "derived" (the catalog derives it from its attributes) or
	// "manual" (the user enters it).
	Source entity.CodeSource `json:"source"`
	// Prefix starts the codes of a series, e.g. "ORG".
	Prefix string `json:"prefix,omitempty"`
}

// BindCode publishes the code rule of a catalog: def.Code, and a required
// code field when the user enters the code.
func BindCode(def *EntityDef, identity entity.Definition) {
	if identity.Kind != entity.CatalogKind {
		return
	}
	code := &CodeAssignment{Source: identity.CodeSource()}
	if code.Source == entity.CodeFromSeries {
		code.Prefix = identity.CodeSeries().Prefix
	}
	def.Code = code
	if code.Source != entity.CodeManual {
		return
	}
	for i := range def.Fields {
		if def.Fields[i].Name == "code" {
			def.Fields[i].Required = true
		}
	}
}
