package entity

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// Document is the base type for business transactions (Документы).
// Examples: Invoice, GoodsReceipt, Payment, Order.
type Document struct {
	BaseDocument

	// Number is the document number (auto-generated, unique within type+period)
	Number string `db:"number" json:"number" meta:"label:Номер"`

	// Date is the business date of the document
	Date time.Time `db:"date" json:"date" meta:"label:Дата"`

	// Posted indicates if document movements are recorded in registers
	Posted bool `db:"posted" json:"posted" meta:"label:Проведен"`

	// PostedVersion tracks posting iterations for movement reconciliation
	// Incremented each time document is posted/modified while posted
	PostedVersion int `db:"posted_version" json:"postedVersion"`

	// OrganizationID is the owning organization (required for multi-org support)
	OrganizationID id.ID `db:"organization_id" json:"organizationId" meta:"label:Организация"`

	// Description is an optional user comment
	Description string `db:"description" json:"description,omitempty" meta:"label:Комментарий"`
}

// NewDocument creates a new Document with generated ID.
// In Database-per-Tenant architecture, tenantID is not required.
func NewDocument(organizationID id.ID) Document {
	return Document{
		BaseDocument:   NewBaseDocument(),
		Date:           time.Now().UTC(),
		OrganizationID: organizationID,
	}
}

// Validate implements Validatable interface.
func (d *Document) Validate(ctx context.Context) error {
	if id.IsNil(d.OrganizationID) {
		return apperror.NewValidation("organization is required").
			WithDetail("field", "organizationId")
	}

	if d.Date.IsZero() {
		return apperror.NewValidation("date is required").
			WithDetail("field", "date")
	}

	return nil
}

// State returns the current lifecycle state of the document (State pattern).
// The state is derived from the Posted and DeletionMark flags.
func (d *Document) State() DocumentState {
	return ResolveDocumentState(d.Posted, d.DeletionMark)
}

// CanModify checks if document can be modified.
// Delegates to the current lifecycle state.
func (d *Document) CanModify() error {
	return d.State().CanModify()
}

// MarkPosted sets the posted flag and increments version.
func (d *Document) MarkPosted() {
	d.Posted = true
	d.PostedVersion++
}

// MarkUnposted clears the posted flag.
func (d *Document) MarkUnposted() {
	d.Posted = false
}

// IsBackdated checks if document date is in the past.
func (d *Document) IsBackdated() bool {
	return d.Date.Before(time.Now().UTC().Truncate(24 * time.Hour))
}

// --- Postable interface default implementations ---
// These methods provide default implementations for the Postable interface.
// Document-specific types only need to implement GetDocumentType() and GenerateMovements().

// GetID returns the document ID (Postable interface).
func (d *Document) GetID() id.ID {
	return d.ID
}

// GetPostedVersion returns the current posting version (Postable interface).
func (d *Document) GetPostedVersion() int {
	return d.PostedVersion
}

// IsPosted returns true if document is currently posted (Postable interface).
func (d *Document) IsPosted() bool {
	return d.Posted
}

// GetNumber returns the document number.
func (d *Document) GetNumber() string {
	return d.Number
}

// SetNumber sets the document number.
func (d *Document) SetNumber(n string) {
	d.Number = n
}

// GetOrganizationID returns the organization ID.
func (d *Document) GetOrganizationID() id.ID {
	return d.OrganizationID
}

// GetRLSDimensions implements security.RLSDimensionable.
// Base implementation returns the organization dimension.
// Document-specific types should override to add extra dimensions (supplier, customer, etc.).
func (d *Document) GetRLSDimensions() map[string]string {
	return map[string]string{
		"organization": d.OrganizationID.String(),
	}
}

// CanPost validates if document can be posted (Postable interface default).
// Delegates state check to the current lifecycle state, then validates entity invariants.
// Override in specific document types if additional validation is needed.
func (d *Document) CanPost(ctx context.Context) error {
	if err := d.State().CanPost(); err != nil {
		return err
	}
	return d.Validate(ctx)
}
