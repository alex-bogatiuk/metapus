package goods_issue

import (
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// Builder provides a fluent API for constructing GoodsIssue documents.
// Designed for tests, seeds, and any programmatic document creation.
//
// Usage:
//
//	doc := goods_issue.NewBuilder(orgID, customerID, warehouseID).
//	    WithCurrency(rubID).
//	    WithDescription("Отгрузка канцтоваров").
//	    AddLine(productID, unitID, 5, 12000, vatRateID, 20).
//	    Build()
type Builder struct {
	doc *GoodsIssue
}

// NewBuilder creates a new GoodsIssue builder with required fields.
func NewBuilder(organizationID, customerID, warehouseID id.ID) *Builder {
	return &Builder{
		doc: NewGoodsIssue(organizationID, customerID, warehouseID),
	}
}

// WithID sets a specific document ID (useful for deterministic tests).
func (b *Builder) WithID(docID id.ID) *Builder {
	b.doc.ID = docID
	return b
}

// WithDate sets the document date.
func (b *Builder) WithDate(t time.Time) *Builder {
	b.doc.Date = t
	return b
}

// WithNumber sets the document number (skips auto-generation).
func (b *Builder) WithNumber(number string) *Builder {
	b.doc.Number = number
	return b
}

// WithDescription sets the document description.
func (b *Builder) WithDescription(desc string) *Builder {
	b.doc.Description = desc
	return b
}

// WithCurrency sets the document currency explicitly.
func (b *Builder) WithCurrency(currencyID id.ID) *Builder {
	b.doc.CurrencyID = currencyID
	return b
}

// WithContract sets the contract reference.
func (b *Builder) WithContract(contractID *id.ID) *Builder {
	b.doc.ContractID = contractID
	return b
}

// WithCustomerOrder sets the customer order reference.
func (b *Builder) WithCustomerOrder(number string, date *time.Time) *Builder {
	b.doc.CustomerOrderNumber = number
	b.doc.CustomerOrderDate = date
	return b
}

// WithAmountIncludesVAT sets the VAT-inclusive flag.
func (b *Builder) WithAmountIncludesVAT(v bool) *Builder {
	b.doc.AmountIncludesVAT = v
	return b
}

// WithCreatedBy sets the audit CreatedBy/UpdatedBy fields.
func (b *Builder) WithCreatedBy(userID id.ID) *Builder {
	b.doc.CreatedBy = userID
	b.doc.UpdatedBy = userID
	return b
}

// AddLine adds a line with common defaults (coefficient=1, discount=0).
// quantity is in human units (e.g., 5 means 5 pieces).
// unitPrice is in minor units (e.g., 12000 = 120.00 RUB).
func (b *Builder) AddLine(productID, unitID id.ID, quantity int, unitPrice types.MinorUnits, vatRateID id.ID, vatPercent int) *Builder {
	b.doc.AddLine(
		productID,
		unitID,
		decimal.NewFromInt(1), // coefficient
		types.NewQuantityFromFloat64(float64(quantity)),
		unitPrice,
		vatRateID,
		vatPercent,
		decimal.Zero, // discountPercent
	)
	return b
}

// AddLineDetailed adds a line with full control over all fields.
func (b *Builder) AddLineDetailed(
	productID, unitID id.ID,
	coefficient decimal.Decimal,
	quantity types.Quantity,
	unitPrice types.MinorUnits,
	vatRateID id.ID,
	vatPercent int,
	discountPercent decimal.Decimal,
) *Builder {
	b.doc.AddLine(productID, unitID, coefficient, quantity, unitPrice, vatRateID, vatPercent, discountPercent)
	return b
}

// Build returns the constructed GoodsIssue.
func (b *Builder) Build() *GoodsIssue {
	return b.doc
}

// MustBuild returns the constructed GoodsIssue after basic sanity checks.
// Panics if required references are nil (for use in tests).
func (b *Builder) MustBuild() *GoodsIssue {
	doc := b.doc
	if id.IsNil(doc.OrganizationID) {
		panic("goods_issue.Builder: organizationID is required")
	}
	if id.IsNil(doc.CustomerID) {
		panic("goods_issue.Builder: customerID is required")
	}
	if id.IsNil(doc.WarehouseID) {
		panic("goods_issue.Builder: warehouseID is required")
	}
	if len(doc.Lines) == 0 {
		panic("goods_issue.Builder: at least one line is required")
	}
	return doc
}

// --- Helpers for tests ---

// NewTestLine creates a minimal line for unit tests.
func NewTestLine(productID, unitID, vatRateID id.ID) GoodsIssueLine {
	return GoodsIssueLine{
		LineID:      id.New(),
		LineNo:      1,
		ProductID:   productID,
		UnitID:      unitID,
		Coefficient: decimal.NewFromInt(1),
		Quantity:    types.NewQuantityFromFloat64(1),
		UnitPrice:   10000, // 100.00 in minor units (RUB kopecks)
		VATRateID:   vatRateID,
		VATAmount:   2000,
		Amount:      12000,
	}
}

// Ensure GoodsIssueLine satisfies ValidatableDocLine at compile time.
var _ interface {
	GetProductID() id.ID
	GetUnitID() id.ID
	GetCoefficient() decimal.Decimal
	GetQuantity() types.Quantity
	GetVATRateID() id.ID
} = GoodsIssueLine{}

// Ensure *GoodsIssue satisfies entity.Validatable at compile time.
var _ entity.Validatable = (*GoodsIssue)(nil)
