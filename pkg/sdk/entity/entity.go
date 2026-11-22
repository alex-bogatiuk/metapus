// Package entity is the model of catalogs, documents and register movements
// a configuration declares (aliases of the platform's model).
package entity

import (
	core "metapus/internal/core/entity"
)

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

type (
	// ActionAvailability is an advisory, actor-specific read projection. A command
	// must authorize again; this value is never accepted in a mutation DTO.
	ActionAvailability = core.ActionAvailability
	// Attributes represents JSONB custom fields with type-safe accessors.
	// Implements sql.Scanner and driver.Valuer for PostgreSQL JSONB mapping.
	//
	// CRITICAL: Uses json.Number to preserve numeric precision.
	// Default Go JSON decoder converts numbers to float64, losing precision for decimals.
	Attributes = core.Attributes
)

const (
	// CodeDerived means the catalog's own hooks derive the code from its
	// attributes (an ISO code, a ticker); the platform only requires the
	// result to be non-empty.
	CodeDerived = core.CodeDerived
	// PartSeparator joins a header table name and a part key. It is reserved for
	// physical table parts: no other table name may contain it.
	PartSeparator = core.PartSeparator
)

// NewCatalog creates a new Catalog with generated ID.
// In Database-per-Tenant architecture, tenantID is not required.
func NewCatalog(code string, name string) core.Catalog { return core.NewCatalog(code, name) }
