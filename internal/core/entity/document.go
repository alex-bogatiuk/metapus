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
	Number string `db:"number" json:"number"`

	// Date is the business date of the document
	Date time.Time `db:"date" json:"date"`

	// Posted indicates if document movements are recorded in registers
	Posted bool `db:"posted" json:"posted"`

	// PostedVersion tracks posting iterations for movement reconciliation
	// Incremented each time document is posted/modified while posted
	PostedVersion int `db:"posted_version" json:"postedVersion"`

	// OrganizationID is the owning organization (required for multi-org support)
	OrganizationID string `db:"organization_id" json:"organizationId"`

	// Comment is an optional user comment
	Comment string `db:"comment" json:"comment,omitempty"`
}

// NewDocument creates a new Document with generated ID.
// In Database-per-Tenant architecture, tenantID is not required.
func NewDocument(organizationID string) Document {
	return Document{
		BaseDocument:   NewBaseDocument(),
		Date:           time.Now().UTC(),
		OrganizationID: organizationID,
	}
}

// Validate implements Validatable interface.
func (d *Document) Validate(ctx context.Context) error {
	if d.OrganizationID == "" {
		return apperror.NewValidation("organization is required").
			WithDetail("field", "organizationId")
	}

	if d.Date.IsZero() {
		return apperror.NewValidation("date is required").
			WithDetail("field", "date")
	}

	return nil
}

// CanModify checks if document can be modified.
// Posted documents require unposting first.
func (d *Document) CanModify() error {
	if d.Posted {
		return apperror.NewBusinessRule(
			apperror.CodeDocumentPosted,
			"Cannot modify posted document. Unpost first.",
		).WithDetail("document_id", d.ID.String())
	}
	return nil
}

// MarkPosted sets the posted flag and increments version.
func (d *Document) MarkPosted() {
	d.Posted = true
	d.PostedVersion++
	d.Touch()
}

// MarkUnposted clears the posted flag.
func (d *Document) MarkUnposted() {
	d.Posted = false
	d.Touch()
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

// CanPost validates if document can be posted (Postable interface default).
// Override in specific document types if additional validation is needed.
func (d *Document) CanPost(ctx context.Context) error {
	return d.Validate(ctx)
}
