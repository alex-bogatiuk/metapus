// Package goods_receipt provides the GoodsReceipt document (Поступление товаров).
package goods_receipt

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

// GoodsReceipt represents a goods receipt document (Поступление товаров).
// Records incoming goods from suppliers into warehouses.
type GoodsReceipt struct {
	entity.Document

	// Supplier reference
	SupplierID id.ID `db:"supplier_id" json:"supplierId"`

	// Warehouse where goods are received
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId"`

	// Supplier's document reference
	SupplierDocNumber string     `db:"supplier_doc_number" json:"supplierDocNumber,omitempty"`
	SupplierDocDate   *time.Time `db:"supplier_doc_date" json:"supplierDocDate,omitempty"`

	// Currency
	Currency string `db:"currency" json:"currency"`

	// Totals (calculated from lines)
	TotalQuantity float64 `db:"total_quantity" json:"totalQuantity"`
	TotalAmount   int64   `db:"total_amount" json:"totalAmount"` // in minor units
	TotalVAT      int64   `db:"total_vat" json:"totalVat"`

	// Table part: received goods
	Lines []GoodsReceiptLine `db:"-" json:"lines"`
}

// GoodsReceiptLine represents a line in the goods receipt.
type GoodsReceiptLine struct {
	// Line identification
	LineID id.ID `db:"line_id" json:"lineId"`
	LineNo int   `db:"line_no" json:"lineNo"`

	// Product reference
	ProductID id.ID `db:"product_id" json:"productId"`

	// Quantity and pricing
	Quantity  float64 `db:"quantity" json:"quantity"`
	UnitPrice int64   `db:"unit_price" json:"unitPrice"` // in minor units
	VATRate   string  `db:"vat_rate" json:"vatRate"`     // "0", "10", "20"
	VATAmount int64   `db:"vat_amount" json:"vatAmount"`
	Amount    int64   `db:"amount" json:"amount"` // total with VAT
}

// NewGoodsReceipt creates a new goods receipt document.
func NewGoodsReceipt(organizationID string, supplierID, warehouseID id.ID) *GoodsReceipt {
	return &GoodsReceipt{
		Document:    entity.NewDocument(organizationID),
		SupplierID:  supplierID,
		WarehouseID: warehouseID,
		Currency:    "RUB",
		Lines:       make([]GoodsReceiptLine, 0),
	}
}

// AddLine adds a line to the goods receipt and recalculates totals.
func (g *GoodsReceipt) AddLine(productID id.ID, quantity float64, unitPrice int64, vatRate string) {
	lineNo := len(g.Lines) + 1

	// Calculate VAT
	vatPercent := vatRateToPercent(vatRate)
	baseAmount := int64(float64(unitPrice) * quantity)
	vatAmount := baseAmount * int64(vatPercent) / 100
	totalAmount := baseAmount + vatAmount

	line := GoodsReceiptLine{
		LineID:    id.New(),
		LineNo:    lineNo,
		ProductID: productID,
		Quantity:  quantity,
		UnitPrice: unitPrice,
		VATRate:   vatRate,
		VATAmount: vatAmount,
		Amount:    totalAmount,
	}

	g.Lines = append(g.Lines, line)
	g.recalculateTotals()
}

// recalculateTotals updates document totals from lines.
func (g *GoodsReceipt) recalculateTotals() {
	g.TotalQuantity = 0
	g.TotalAmount = 0
	g.TotalVAT = 0

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

	if id.IsNil(g.SupplierID) {
		return apperror.NewValidation("supplier is required").
			WithDetail("field", "supplierId")
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
		if line.Quantity <= 0 {
			return apperror.NewValidation("quantity must be positive").
				WithDetail("field", "lines").
				WithDetail("lineNo", i+1)
		}
	}

	return nil
}

// --- Postable interface implementation ---
// GetID, GetPostedVersion, IsPosted, CanPost are inherited from entity.Document

// GetDocumentType returns the document type name.
func (g *GoodsReceipt) GetDocumentType() string {
	return "GoodsReceipt"
}

// GenerateMovements creates register movements for this document.
func (g *GoodsReceipt) GenerateMovements(ctx context.Context) (*posting.MovementSet, error) {
	movements := posting.NewMovementSet()

	newVersion := g.PostedVersion + 1

	for _, line := range g.Lines {
		// Stock movement: receipt to warehouse
		stockMovement := entity.NewStockMovement(
			g.ID,
			g.GetDocumentType(),
			newVersion,
			g.Date,
			entity.RecordTypeReceipt,
			g.WarehouseID,
			line.ProductID,
			types.NewQuantityFromFloat64(line.Quantity),
		)

		movements.AddStock(stockMovement)
	}

	return movements, nil
}

// Helper function
func vatRateToPercent(rate string) int {
	switch rate {
	case "10":
		return 10
	case "20":
		return 20
	default:
		return 0
	}
}

// Ensure interface compliance at compile time.
var _ posting.Postable = (*GoodsReceipt)(nil)
