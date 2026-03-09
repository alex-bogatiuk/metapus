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
	"metapus/internal/domain" // <--- Added import
	"metapus/internal/domain/posting"
)

// GoodsIssue represents a goods issue document (Расход товаров).
// Records outgoing goods to customers from warehouses.
type GoodsIssue struct {
	entity.Document

	// Customer reference
	CustomerID id.ID `db:"customer_id" json:"customerId" meta:"label:Покупатель"`

	// Contract / Agreement reference
	ContractID *id.ID `db:"contract_id" json:"contractId,omitempty" meta:"label:Договор"`

	// Warehouse from which goods are issued
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId" meta:"label:Склад"`

	// Customer order reference
	CustomerOrderNumber string     `db:"customer_order_number" json:"customerOrderNumber,omitempty" meta:"label:№ заказа покупателя"`
	CustomerOrderDate   *time.Time `db:"customer_order_date" json:"customerOrderDate,omitempty" meta:"label:Дата заказа покупателя"`

	// Currency support trait
	entity.CurrencyAware

	// AmountIncludesVAT indicates whether prices are VAT-inclusive (gross) or VAT-exclusive (net)
	AmountIncludesVAT bool `db:"amount_includes_vat" json:"amountIncludesVat" meta:"label:Сумма включает НДС"`

	// Totals (calculated from lines)
	TotalQuantity types.Quantity   `db:"total_quantity" json:"totalQuantity" meta:"label:Количество итого"`
	TotalAmount   types.MinorUnits `db:"total_amount" json:"totalAmount" meta:"label:Сумма итого"`
	TotalVAT      types.MinorUnits `db:"total_vat" json:"totalVat" meta:"label:НДС итого"`

	// Table part: issued goods
	Lines []GoodsIssueLine `db:"-" json:"lines" meta:"label:Товары"`
}

// GoodsIssueLine represents a line in the goods issue.
type GoodsIssueLine struct {
	// Line identification
	LineID id.ID `db:"line_id" json:"lineId"`
	LineNo int   `db:"line_no" json:"lineNo" meta:"label:№ строки"`

	// Product reference
	ProductID id.ID `db:"product_id" json:"productId" meta:"label:Номенклатура"`

	// Unit of measurement (e.g., box, pallet)
	UnitID id.ID `db:"unit_id" json:"unitId" meta:"label:Единица"`

	// Coefficient for conversion to base unit (e.g., 12 if 1 box = 12 pcs)
	Coefficient decimal.Decimal `db:"coefficient" json:"coefficient" meta:"label:Коэффициент"`

	// Quantity in UnitID
	Quantity types.Quantity `db:"quantity" json:"quantity" meta:"label:Количество"`

	// Price per UnitID (in minor units)
	UnitPrice types.MinorUnits `db:"unit_price" json:"unitPrice" meta:"label:Цена"`

	// Discount
	DiscountPercent decimal.Decimal  `db:"discount_percent" json:"discountPercent" meta:"label:Скидка %"`
	DiscountAmount  types.MinorUnits `db:"discount_amount" json:"discountAmount" meta:"label:Скидка сумма"`

	// VAT (reference to cat_vat_rates)
	VATRateID id.ID            `db:"vat_rate_id" json:"vatRateId" meta:"label:Ставка НДС"`
	VATAmount types.MinorUnits `db:"vat_amount" json:"vatAmount" meta:"label:Сумма НДС"`

	// Total amount for this line
	Amount types.MinorUnits `db:"amount" json:"amount" meta:"label:Сумма"`
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

	// Common line validation strategy
	return domain.ValidateDocumentLines(g.Lines)
}

// --- LinesAccessor implementation ---

// GetLines returns the document lines.
func (g *GoodsIssue) GetLines() []GoodsIssueLine {
	return g.Lines
}

// SetLines replaces the document lines.
func (g *GoodsIssue) SetLines(lines []GoodsIssueLine) {
	g.Lines = lines
}

// --- CurrencyAwareDoc implementation ---

// GetContractID returns the contract ID (may be nil).
func (g *GoodsIssue) GetContractID() *id.ID {
	return g.ContractID
}

// --- ValidatableDocLine implementation for GoodsIssueLine ---

func (l GoodsIssueLine) GetProductID() id.ID             { return l.ProductID }
func (l GoodsIssueLine) GetUnitID() id.ID                { return l.UnitID }
func (l GoodsIssueLine) GetCoefficient() decimal.Decimal { return l.Coefficient }
func (l GoodsIssueLine) GetQuantity() types.Quantity     { return l.Quantity }
func (l GoodsIssueLine) GetVATRateID() id.ID             { return l.VATRateID }

// --- Postable interface implementation ---
// GetID, GetPostedVersion, IsPosted, CanPost, MarkPosted, MarkUnposted are inherited from entity.Document

func (g *GoodsIssue) GetDocumentType() string { return "GoodsIssue" }

// GenerateStockMovements implements posting.StockMovementSource.
// Creates EXPENSE movements (reduces stock) — quantity in base units: line.Quantity * line.Coefficient.
func (g *GoodsIssue) GenerateStockMovements(ctx context.Context) ([]entity.StockMovement, error) {
	newVersion := g.PostedVersion + 1
	movements := make([]entity.StockMovement, 0, len(g.Lines))

	for _, line := range g.Lines {
		// Convert to base unit quantity: Quantity * Coefficient
		baseQtyDecimal := decimal.NewFromInt(line.Quantity.Int64Scaled()).Mul(line.Coefficient)
		baseQty := types.NewQuantityFromInt64Scaled(baseQtyDecimal.IntPart())

		movements = append(movements, entity.NewStockMovement(
			g.ID,
			g.GetDocumentType(),
			newVersion,
			g.Date,
			entity.RecordTypeExpense,
			g.WarehouseID,
			line.ProductID,
			baseQty,
		))
	}

	return movements, nil
}

// Ensure interface compliance at compile time.
var _ posting.Postable = (*GoodsIssue)(nil)
var _ posting.StockMovementSource = (*GoodsIssue)(nil)
