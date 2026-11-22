// Package entity is the model of catalogs, documents and register movements
// a configuration declares (aliases of the platform's model).
package entity

import core "metapus/internal/core/entity"

// Definitions.
type (
	Definition         = core.Definition
	DefinitionProvider = core.DefinitionProvider
	Kind               = core.Kind
	Part               = core.Part
	CodeRule           = core.CodeRule
	NumberRule         = core.NumberRule
	CatalogMeta        = core.CatalogMeta
	HierarchyType      = core.HierarchyType
)

const (
	CatalogKind  = core.CatalogKind
	DocumentKind = core.DocumentKind

	HierarchyGroupsAndItems = core.HierarchyGroupsAndItems
	HierarchyItemsOnly      = core.HierarchyItemsOnly
)

// Catalogs and documents: a model embeds one of them.
type (
	Catalog  = core.Catalog
	Document = core.Document
)

// NewDocument returns a new document with its identity.
func NewDocument() Document { return core.NewDocument() }

// Register movements.
type (
	RecordType        = core.RecordType
	MovementBase      = core.MovementBase
	MovementProvider  = core.MovementProvider
	DocumentMovement  = core.DocumentMovement
	MovementColumnDef = core.MovementColumnDef
	MovementRefValue  = core.MovementRefValue
)

const (
	RecordTypeReceipt = core.RecordTypeReceipt
	RecordTypeExpense = core.RecordTypeExpense
)

// NewMovementBase returns the common part of a register movement.
var NewMovementBase = core.NewMovementBase
