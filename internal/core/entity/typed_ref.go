package entity

import (
	"context"
	"strings"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// TypedRef is a universal polymorphic reference trait for linking to any registered entity.
// Stores entity type name (EntityName from metadata registry) + entity UUID.
//
// Analogues:
//   - 1C: ОписаниеТипов / СоставнойТипДанных — composite type with _Type + _IDRRef
//   - ERPNext: Dynamic Link — link_doctype + link_name
//   - SAP: OBJECTTYPE + OBJECTKEY
//   - Odoo: fields.Reference — "model,id"
//
// Works with BOTH catalogs and documents:
//
//	type BankStatementLine struct {
//	    entity.TypedRef  // embeds RefType + RefID
//	    Amount types.MinorUnits
//	}
//
//	// Restricted: only specific document types allowed
//	var basisTypes = []string{"CashReceipt", "CashPayment"}
//	func (l BankStatementLine) Validate(ctx) error { return l.ValidateRef(ctx, basisTypes) }
//
//	// Universal: any registered entity allowed (like 1C "Произвольный")
//	func (l SomeOtherLine) Validate(ctx) error { return l.ValidateRef(ctx, nil) }
//
// SQL schema:
//
//	ref_type VARCHAR(50) NOT NULL,
//	ref_id   UUID NOT NULL,
//	CREATE INDEX ON xxx (ref_type, ref_id);
type TypedRef struct {
	// RefType is the entity type name — matches EntityName from metadata registry.
	// Can be a document ("GoodsReceipt", "CashPayment") or catalog ("Counterparty", "Organization").
	RefType string `db:"ref_type" json:"refType" meta:"label:Тип"`

	// RefID is the UUID of the referenced entity.
	RefID id.ID `db:"ref_id" json:"refId" meta:"label:Ссылка"`
}

// NewTypedRef creates a typed reference to a specific entity.
func NewTypedRef(refType string, refID id.ID) TypedRef {
	return TypedRef{
		RefType: refType,
		RefID:   refID,
	}
}

// ValidateRef checks that the reference is complete and points to an allowed entity type.
//
// allowedTypes controls which entity types are permitted:
//   - nil / empty slice → ANY registered entity type is allowed (1C "Произвольный")
//   - non-empty slice → only listed types are allowed (1C "Ограниченный набор типов")
func (r *TypedRef) ValidateRef(ctx context.Context, allowedTypes []string) error {
	if r.RefType == "" {
		return apperror.NewValidation("reference type is required").
			WithDetail("field", "refType")
	}
	if id.IsNil(r.RefID) {
		return apperror.NewValidation("reference ID is required").
			WithDetail("field", "refId")
	}

	// nil or empty allowedTypes = any type is accepted (universal/arbitrary reference)
	if len(allowedTypes) == 0 {
		return nil
	}

	for _, t := range allowedTypes {
		if r.RefType == t {
			return nil
		}
	}

	return apperror.NewValidation("invalid reference type: "+r.RefType).
		WithDetail("field", "refType").
		WithDetail("allowed", strings.Join(allowedTypes, ", "))
}

// IsRefType checks if the reference points to a specific entity type.
func (r *TypedRef) IsRefType(refType string) bool {
	return r.RefType == refType
}

// IsEmpty returns true if the reference is not set (zero values).
func (r *TypedRef) IsEmpty() bool {
	return r.RefType == "" && id.IsNil(r.RefID)
}

// GetRefType returns the entity type string.
func (r *TypedRef) GetRefType() string {
	return r.RefType
}

// GetRefID returns the referenced entity ID.
func (r *TypedRef) GetRefID() id.ID {
	return r.RefID
}
