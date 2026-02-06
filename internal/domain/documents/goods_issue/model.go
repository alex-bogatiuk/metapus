// Package goods_issue provides the GoodsIssue document (Расход товаров).
package goods_issue

import (
	"context"
	"time"

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

	// Warehouse from which goods are issued
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId"`

	// Customer order reference
	CustomerOrderNumber string     `db:"customer_order_number" json:"customerOrderNumber,omitempty"`
	CustomerOrderDate   *time.Time `db:"customer_order_date" json:"customerOrderDate,omitempty"`

	// Currency support trait
	entity.CurrencyAware

	// Totals (calculated from lines)
	TotalQuantity types.Quantity   `db:"total_quantity" json:"totalQuantity"`
	TotalAmount   types.MinorUnits `db:"total_amount" json:"totalAmount"`
	TotalVAT      types.MinorUnits `db:"total_vat" json:"totalVat"`

	// Table part: issued goods
	Lines []GoodsIssueLine `db:"-" json:"lines"`
}

// GoodsIssueLine represents a line in the goods issue.
type GoodsIssueLine struct {
	LineID id.ID `db:"line_id" json:"lineId"`
	LineNo int   `db:"line_no" json:"lineNo"`

	ProductID id.ID            `db:"product_id" json:"productId"`
	Quantity  types.Quantity   `db:"quantity" json:"quantity"`
	UnitPrice types.MinorUnits `db:"unit_price" json:"unitPrice"`
	VATRate   string           `db:"vat_rate" json:"vatRate"`
	VATAmount types.MinorUnits `db:"vat_amount" json:"vatAmount"`
	Amount    types.MinorUnits `db:"amount" json:"amount"`
}

// NewGoodsIssue creates a new goods issue document.
func NewGoodsIssue(organizationID id.ID, customerID, warehouseID id.ID) *GoodsIssue {
	return &GoodsIssue{
		Document:    entity.NewDocument(organizationID),
		CustomerID:  customerID,
		WarehouseID: warehouseID,
		Lines:       make([]GoodsIssueLine, 0),
	}
}

// AddLine adds a line to the goods issue and recalculates totals.
func (g *GoodsIssue) AddLine(productID id.ID, quantity types.Quantity, unitPrice types.MinorUnits, vatRate string) {
	lineNo := len(g.Lines) + 1

	// Quantity is scaled by 10000. UnitPrice is in minor units.
	// baseAmount (minor units) = (QuantityScaled * UnitPrice) / 10000
	vatPercent := vatRateToPercent(vatRate)
	baseAmount := types.MinorUnits((quantity.Int64Scaled() * int64(unitPrice)) / 10000)
	vatAmount := baseAmount * types.MinorUnits(vatPercent) / 100
	totalAmount := baseAmount + vatAmount

	line := GoodsIssueLine{
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
		if line.Quantity <= 0 {
			return apperror.NewValidation("quantity must be positive").
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
func (g *GoodsIssue) GenerateMovements(ctx context.Context) (*posting.MovementSet, error) {
	movements := posting.NewMovementSet()
	newVersion := g.PostedVersion + 1

	for _, line := range g.Lines {
		// Stock movement: expense from warehouse
		stockMovement := entity.NewStockMovement(
			g.ID,
			g.GetDocumentType(),
			newVersion,
			g.Date,
			entity.RecordTypeExpense, // <-- KEY DIFFERENCE from GoodsReceipt
			g.WarehouseID,
			line.ProductID,
			line.Quantity,
		)

		movements.AddStock(stockMovement)
	}

	return movements, nil
}

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

var _ posting.Postable = (*GoodsIssue)(nil)
