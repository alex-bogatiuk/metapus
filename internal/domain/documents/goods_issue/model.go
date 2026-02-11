// Package goods_issue provides the GoodsIssue document (Расход товаров).
package goods_issue

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

// GoodsIssue represents a goods issue document (Расход товаров).
// Records outgoing goods to customers from warehouses.
type GoodsIssue struct {
	entity.Document

	// Customer reference
	CustomerID id.ID `db:"customer_id" json:"customerId"`

	// Contract / Agreement reference
	ContractID *id.ID `db:"contract_id" json:"contractId,omitempty"`

	// Warehouse from which goods are issued
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId"`

	// Customer order reference
	CustomerOrderNumber string     `db:"customer_order_number" json:"customerOrderNumber,omitempty"`
	CustomerOrderDate   *time.Time `db:"customer_order_date" json:"customerOrderDate,omitempty"`

	// Currency support trait
	entity.CurrencyAware

	// AmountIncludesVAT indicates whether prices are VAT-inclusive (gross) or VAT-exclusive (net)
	AmountIncludesVAT bool `db:"amount_includes_vat" json:"amountIncludesVat"`

	// Totals (calculated from lines)
	TotalQuantity types.Quantity   `db:"total_quantity" json:"totalQuantity"`
	TotalAmount   types.MinorUnits `db:"total_amount" json:"totalAmount"`
	TotalVAT      types.MinorUnits `db:"total_vat" json:"totalVat"`

	// Table part: issued goods
	Lines []GoodsIssueLine `db:"-" json:"lines"`
}

// GoodsIssueLine represents a line in the goods issue.
type GoodsIssueLine struct {
	// Line identification
	LineID id.ID `db:"line_id" json:"lineId"`
	LineNo int   `db:"line_no" json:"lineNo"`

	// Product reference
	ProductID id.ID `db:"product_id" json:"productId"`

	// Unit of measurement (e.g., box, pallet)
	UnitID id.ID `db:"unit_id" json:"unitId"`

	// Coefficient for conversion to base unit (e.g., 12 if 1 box = 12 pcs)
	Coefficient decimal.Decimal `db:"coefficient" json:"coefficient"`

	// Quantity in UnitID
	Quantity types.Quantity `db:"quantity" json:"quantity"`

	// Price per UnitID (in minor units)
	UnitPrice types.MinorUnits `db:"unit_price" json:"unitPrice"`

	// Discount
	DiscountPercent decimal.Decimal  `db:"discount_percent" json:"discountPercent"`
	DiscountAmount  types.MinorUnits `db:"discount_amount" json:"discountAmount"`

	// VAT (reference to cat_vat_rates)
	VATRateID id.ID            `db:"vat_rate_id" json:"vatRateId"`
	VATAmount types.MinorUnits `db:"vat_amount" json:"vatAmount"`

	// Total amount for this line
	Amount types.MinorUnits `db:"amount" json:"amount"`
}

// NewGoodsIssue creates a new goods issue document.
func NewGoodsIssue(organizationID id.ID, customerID, warehouseID id.ID) *GoodsIssue {
	return &GoodsIssue{
		Document:          entity.NewDocument(organizationID),
		CustomerID:        customerID,
		WarehouseID:       warehouseID,
		AmountIncludesVAT: false,
		Lines:             make([]GoodsIssueLine, 0),
	}
}

// AddLine adds a line to the goods issue and recalculates totals.
func (g *GoodsIssue) AddLine(
	productID id.ID,
	unitID id.ID,
	coefficient decimal.Decimal,
	quantity types.Quantity,
	unitPrice types.MinorUnits,
	vatRateID id.ID,
	vatPercent int,
	discountPercent decimal.Decimal,
) {
	lineNo := len(g.Lines) + 1

	// Ensure coefficient is at least 1
	if coefficient.LessThanOrEqual(decimal.Zero) {
		coefficient = decimal.NewFromInt(1)
	}

	// All intermediate calculations use decimal.Decimal to avoid truncation.
	// Final results are rounded to nearest integer (banker's rounding).
	scaleDec := decimal.NewFromInt(types.QuantityScale)
	qtyDec := decimal.NewFromInt(quantity.Int64Scaled())
	priceDec := decimal.NewFromInt(int64(unitPrice))

	// baseAmount = quantity * unitPrice (quantity is scaled by 10000)
	baseAmountDec := qtyDec.Mul(priceDec).Div(scaleDec)

	// Apply discount
	discountAmountDec := decimal.Zero
	if discountPercent.IsPositive() {
		discountAmountDec = baseAmountDec.Mul(discountPercent).Div(decimal.NewFromInt(100))
	}
	netAmountDec := baseAmountDec.Sub(discountAmountDec)
	discountAmount := types.MinorUnits(discountAmountDec.Round(0).IntPart())
	netAmount := types.MinorUnits(netAmountDec.Round(0).IntPart())

	// Calculate VAT based on AmountIncludesVAT flag
	var vatAmount types.MinorUnits
	var totalAmount types.MinorUnits
	vatPercentDec := decimal.NewFromInt(int64(vatPercent))
	if g.AmountIncludesVAT {
		// Price includes VAT: extract VAT from net amount
		// vatAmount = netAmount * vatPercent / (100 + vatPercent)
		if vatPercent > 0 {
			vatAmountDec := netAmountDec.Mul(vatPercentDec).Div(decimal.NewFromInt(int64(100 + vatPercent)))
			vatAmount = types.MinorUnits(vatAmountDec.Round(0).IntPart())
		}
		totalAmount = netAmount
	} else {
		// Price excludes VAT: add VAT on top
		vatAmountDec := netAmountDec.Mul(vatPercentDec).Div(decimal.NewFromInt(100))
		vatAmount = types.MinorUnits(vatAmountDec.Round(0).IntPart())
		totalAmount = netAmount + vatAmount
	}

	line := GoodsIssueLine{
		LineID:          id.New(),
		LineNo:          lineNo,
		ProductID:       productID,
		UnitID:          unitID,
		Coefficient:     coefficient,
		Quantity:        quantity,
		UnitPrice:       unitPrice,
		DiscountPercent: discountPercent,
		DiscountAmount:  discountAmount,
		VATRateID:       vatRateID,
		VATAmount:       vatAmount,
		Amount:          totalAmount,
	}

	g.Lines = append(g.Lines, line)
	g.recalculateTotals()
}

func (g *GoodsIssue) recalculateTotals() {
	g.TotalQuantity = types.Quantity(0)
	g.TotalAmount = types.MinorUnits(0)
	g.TotalVAT = types.MinorUnits(0)

	for _, line := range g.Lines {
		g.TotalQuantity += line.Quantity
		g.TotalAmount += line.Amount
		g.TotalVAT += line.VATAmount
	}
}

// Validate implements entity.Validatable.
func (g *GoodsIssue) Validate(ctx context.Context) error {
	if err := g.Document.Validate(ctx); err != nil {
		return err
	}

	if err := g.CurrencyAware.ValidateCurrency(ctx); err != nil {
		return err
	}

	if id.IsNil(g.CustomerID) {
		return apperror.NewValidation("customer is required").
			WithDetail("field", "customerId")
	}

	if id.IsNil(g.WarehouseID) {
		return apperror.NewValidation("warehouse is required").
			WithDetail("field", "warehouseId")
	}

	if len(g.Lines) == 0 {
		return apperror.NewValidation("at least one line is required").
			WithDetail("field", "lines")
	}

	for i, line := range g.Lines {
		if id.IsNil(line.ProductID) {
			return apperror.NewValidation("product is required").
				WithDetail("field", "lines").
				WithDetail("lineNo", i+1)
		}
		if id.IsNil(line.UnitID) {
			return apperror.NewValidation("unit is required").
				WithDetail("field", "lines").
				WithDetail("lineNo", i+1)
		}
		if line.Coefficient.LessThanOrEqual(decimal.Zero) {
			return apperror.NewValidation("coefficient must be positive").
				WithDetail("field", "lines").
				WithDetail("lineNo", i+1)
		}
		if line.Quantity <= 0 {
			return apperror.NewValidation("quantity must be positive").
				WithDetail("field", "lines").
				WithDetail("lineNo", i+1)
		}
		if id.IsNil(line.VATRateID) {
			return apperror.NewValidation("VAT rate is required").
				WithDetail("field", "lines").
				WithDetail("lineNo", i+1)
		}
	}

	return nil
}

// --- Postable interface implementation ---
// GetID, GetPostedVersion, IsPosted, CanPost, MarkPosted, MarkUnposted are inherited from entity.Document

func (g *GoodsIssue) GetDocumentType() string { return "GoodsIssue" }

// GenerateMovements creates register movements for this document.
// GoodsIssue creates EXPENSE movements (reduces stock).
// Quantity written to stock register is in base units: line.Quantity * line.Coefficient.
func (g *GoodsIssue) GenerateMovements(ctx context.Context) (*posting.MovementSet, error) {
	movements := posting.NewMovementSet()
	newVersion := g.PostedVersion + 1

	for _, line := range g.Lines {
		// Convert to base unit quantity: Quantity * Coefficient
		baseQtyDecimal := decimal.NewFromInt(line.Quantity.Int64Scaled()).Mul(line.Coefficient)
		baseQty := types.NewQuantityFromInt64Scaled(baseQtyDecimal.IntPart())

		// Stock movement: expense from warehouse (in base units)
		stockMovement := entity.NewStockMovement(
			g.ID,
			g.GetDocumentType(),
			newVersion,
			g.Date,
			entity.RecordTypeExpense, // <-- KEY DIFFERENCE from GoodsReceipt
			g.WarehouseID,
			line.ProductID,
			baseQty,
		)

		movements.AddStock(stockMovement)
	}

	return movements, nil
}

var _ posting.Postable = (*GoodsIssue)(nil)
