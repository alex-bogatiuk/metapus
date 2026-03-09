// Package goods_receipt provides the GoodsReceipt document (Поступление товаров).
package goods_receipt

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain" // Add domain import for ValidateDocumentLines
	"metapus/internal/domain/posting"
)

// GoodsReceipt represents a goods receipt document (Поступление товаров).
// Records incoming goods from suppliers into warehouses.
type GoodsReceipt struct {
	entity.Document

	// Supplier reference
	SupplierID id.ID `db:"supplier_id" json:"supplierId" meta:"label:Поставщик"`

	// Contract / Agreement reference
	ContractID *id.ID `db:"contract_id" json:"contractId,omitempty" meta:"label:Договор"`

	// Warehouse where goods are received
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId" meta:"label:Склад"`

	// Supplier's document reference
	SupplierDocNumber string     `db:"supplier_doc_number" json:"supplierDocNumber,omitempty" meta:"label:№ документа поставщика"`
	SupplierDocDate   *time.Time `db:"supplier_doc_date" json:"supplierDocDate,omitempty" meta:"label:Дата документа поставщика"`

	// Internal incoming document registration number
	IncomingNumber *string `db:"incoming_number" json:"incomingNumber,omitempty" meta:"label:№ вх. документа"`

	// Currency support trait
	entity.CurrencyAware

	// AmountIncludesVAT indicates whether prices are VAT-inclusive (gross) or VAT-exclusive (net)
	AmountIncludesVAT bool `db:"amount_includes_vat" json:"amountIncludesVat" meta:"label:Сумма включает НДС"`

	// Totals (calculated from lines)
	TotalQuantity types.Quantity   `db:"total_quantity" json:"totalQuantity" meta:"label:Количество итого"`
	TotalAmount   types.MinorUnits `db:"total_amount" json:"totalAmount" meta:"label:Сумма итого"`
	TotalVAT      types.MinorUnits `db:"total_vat" json:"totalVat" meta:"label:НДС итого"`

	// Table part: received goods
	Lines []GoodsReceiptLine `db:"-" json:"lines" meta:"label:Товары"`
}

// GoodsReceiptLine represents a line in the goods receipt.
type GoodsReceiptLine struct {
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
	VATRateID  id.ID            `db:"vat_rate_id" json:"vatRateId" meta:"label:Ставка НДС"`
	VATPercent int              `db:"vat_percent" json:"vatPercent" meta:"label:% НДС"`
	VATAmount  types.MinorUnits `db:"vat_amount" json:"vatAmount" meta:"label:Сумма НДС"`

	// Total amount for this line
	Amount types.MinorUnits `db:"amount" json:"amount" meta:"label:Сумма"`
}

func NewGoodsReceipt(organizationID id.ID, supplierID, warehouseID id.ID) *GoodsReceipt {
	return &GoodsReceipt{
		Document:          entity.NewDocument(organizationID),
		SupplierID:        supplierID,
		WarehouseID:       warehouseID,
		AmountIncludesVAT: false,
		Lines:             make([]GoodsReceiptLine, 0),
	}
}

// AddLine adds a line to the goods receipt and recalculates totals.
func (g *GoodsReceipt) AddLine(
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

	line := GoodsReceiptLine{
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
		VATPercent:      vatPercent,
		VATAmount:       vatAmount,
		Amount:          totalAmount,
	}

	g.Lines = append(g.Lines, line)
	g.recalculateTotals()
}

// recalculateTotals updates document totals from lines.
func (g *GoodsReceipt) recalculateTotals() {
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
func (g *GoodsReceipt) Validate(ctx context.Context) error {
	if err := g.Document.Validate(ctx); err != nil {
		return err
	}

	if err := g.CurrencyAware.ValidateCurrency(ctx); err != nil {
		return err
	}

	if id.IsNil(g.SupplierID) {
		return apperror.NewValidation("supplier is required").
			WithDetail("field", "supplierId")
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
func (g *GoodsReceipt) GetLines() []GoodsReceiptLine {
	return g.Lines
}

// SetLines replaces the document lines.
func (g *GoodsReceipt) SetLines(lines []GoodsReceiptLine) {
	g.Lines = lines
}

// --- CurrencyAwareDoc implementation ---

// GetContractID returns the contract ID (may be nil).
func (g *GoodsReceipt) GetContractID() *id.ID {
	return g.ContractID
}

// --- ValidatableDocLine implementation for GoodsReceiptLine ---

func (l GoodsReceiptLine) GetProductID() id.ID             { return l.ProductID }
func (l GoodsReceiptLine) GetUnitID() id.ID                { return l.UnitID }
func (l GoodsReceiptLine) GetCoefficient() decimal.Decimal { return l.Coefficient }
func (l GoodsReceiptLine) GetQuantity() types.Quantity     { return l.Quantity }
func (l GoodsReceiptLine) GetVATRateID() id.ID             { return l.VATRateID }

// --- Postable interface implementation ---
// GetID, GetPostedVersion, IsPosted, CanPost are inherited from entity.Document

// GetDocumentType returns the document type name.
func (g *GoodsReceipt) GetDocumentType() string {
	return "GoodsReceipt"
}

// GenerateStockMovements implements posting.StockMovementSource.
// Creates RECEIPT movements — quantity in base units: line.Quantity * line.Coefficient.
func (g *GoodsReceipt) GenerateStockMovements(ctx context.Context) ([]entity.StockMovement, error) {
	newVersion := g.PostedVersion + 1
	movements := make([]entity.StockMovement, 0, len(g.Lines))

	for _, line := range g.Lines {
		// Convert to base unit quantity: Quantity * Coefficient
		// Quantity is scaled x10000 internally. Coefficient is decimal.
		baseQtyDecimal := decimal.NewFromInt(line.Quantity.Int64Scaled()).Mul(line.Coefficient)
		baseQty := types.NewQuantityFromInt64Scaled(baseQtyDecimal.IntPart())

		movements = append(movements, entity.NewStockMovement(
			g.ID,
			g.GetDocumentType(),
			newVersion,
			g.Date,
			entity.RecordTypeReceipt,
			g.WarehouseID,
			line.ProductID,
			baseQty,
		))
	}

	return movements, nil
}

// Ensure interface compliance at compile time.
var _ posting.Postable = (*GoodsReceipt)(nil)
var _ posting.StockMovementSource = (*GoodsReceipt)(nil)
