// Package metadata describes entities, enums and actions to the user
// interface.
package metadata

import (
	core "metapus/internal/metadata"
)

type (
	// DocumentCapabilities describes which generic document operations are
	// available for an entity. A document can be metadata-driven without exposing
	// the full CRUD/posting surface.
	DocumentCapabilities = core.DocumentCapabilities
	// EntityActionDef describes an explicit custom operation in the same contract
	// as standard document capabilities.
	EntityActionDef = core.EntityActionDef
	// EntityActionInput describes supported typed command inputs, not arbitrary
	// client-generated payloads. reasonedDecision sends only {version, reason}.
	EntityActionInput = core.EntityActionInput
	// EntityActionUI contains explicit generic UI placements. Omitted/false values
	// mean the action is discoverable in metadata but not rendered automatically.
	EntityActionUI = core.EntityActionUI
	// EntityDef describes a business entity.
	EntityDef = core.EntityDef
	// EnumValue represents a single value within an enumeration,
	// conveying both its backend identifier and a human-readable label.
	EnumValue = core.EnumValue
	// FieldDef describes a field.
	FieldDef = core.FieldDef
	// Presentation holds the human-readable display names for an entity.
	// Used by frontend to render titles, buttons, breadcrumbs, etc.
	Presentation = core.Presentation
)

const (
	EntityActionScopeCollection = core.EntityActionScopeCollection
	EntityActionScopeItem       = core.EntityActionScopeItem
	TypeCatalog                 = core.TypeCatalog
	TypeDocument                = core.TypeDocument
	TypeEnum                    = core.TypeEnum
)

// EnumLabel returns the registered human-readable label of an enum value.
// Unregistered types and unknown values report false so callers can choose
// their own fallback instead of displaying an invented label.
func EnumLabel[T ~string](value T) (string, bool) { return core.EnumLabel[T](value) }

// Inspect analyzes a struct and returns its EntityDef.
func Inspect(p0 any, name string, entityType core.EntityType) core.EntityDef {
	return core.Inspect(p0, name, entityType)
}

func NewRegistry() *core.Registry { return core.NewRegistry() }

// RegisterEnum registers a generic, string-based enumeration type.
// The provided slice controls the order in which options render
// in frontend drop-downs and selection menus.
func RegisterEnum[T ~string](values []core.EnumValue) { core.RegisterEnum[T](values) }
