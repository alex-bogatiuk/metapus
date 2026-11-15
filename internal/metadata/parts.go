package metadata

import (
	"fmt"
	"slices"

	"metapus/internal/core/entity"
)

// BindParts attaches the entity's declared SQL table parts to the inspected
// collections. Every inspected collection must be declared either as an SQL
// part or as derived, so no table name is ever guessed from a field name.
func BindParts(def *EntityDef, identity entity.Definition) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	bound := make(map[string]bool, len(def.TableParts))
	for _, part := range identity.Parts {
		i := slices.IndexFunc(def.TableParts, func(tp TablePartDef) bool { return tp.Name == part.Field })
		if i < 0 {
			return fmt.Errorf("entity %q: table part %q binds unknown collection %q", identity.Name, part.Key, part.Field)
		}
		def.TableParts[i].SQLTable = entity.PartTableName(identity.Table, part.Key)
		bound[part.Field] = true
	}
	for _, field := range identity.DerivedParts {
		if !slices.ContainsFunc(def.TableParts, func(tp TablePartDef) bool { return tp.Name == field }) {
			return fmt.Errorf("entity %q: derived part %q is not a collection", identity.Name, field)
		}
		bound[field] = true
	}
	for _, tp := range def.TableParts {
		if !bound[tp.Name] {
			return fmt.Errorf("entity %q: collection %q must be declared in Definition.Parts or DerivedParts", identity.Name, tp.Name)
		}
	}
	return nil
}

// MustBindParts panics on an inconsistent registration, like other startup
// metadata errors.
func MustBindParts(def *EntityDef, identity entity.Definition) {
	if err := BindParts(def, identity); err != nil {
		panic(err)
	}
}
