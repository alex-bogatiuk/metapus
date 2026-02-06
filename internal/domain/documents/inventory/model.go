// Package inventory provides the Inventory document (Инвентаризация).
package inventory

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

// InventoryStatus represents the status of an inventory document.
type InventoryStatus string

const (
	StatusDraft      InventoryStatus = "draft"
	StatusInProgress InventoryStatus = "in_progress"
	StatusCompleted  InventoryStatus = "completed"
	StatusCancelled  InventoryStatus = "cancelled"
)

// Inventory represents an inventory document (Инвентаризация).
type Inventory struct {
	entity.Document

	WarehouseID   id.ID           `db:"warehouse_id" json:"warehouseId"`
	Status        InventoryStatus `db:"status" json:"status"`
	StartDate     time.Time       `db:"start_date" json:"startDate"`
	EndDate       *time.Time      `db:"end_date" json:"endDate,omitempty"`
	ResponsibleID *id.ID          `db:"responsible_id" json:"responsibleId,omitempty"`

	// Totals (calculated)
	TotalBookQuantity     types.Quantity `db:"total_book_quantity" json:"totalBookQuantity"`
	TotalActualQuantity   types.Quantity `db:"total_actual_quantity" json:"totalActualQuantity"`
	TotalSurplusQuantity  types.Quantity `db:"total_surplus_quantity" json:"totalSurplusQuantity"`
	TotalShortageQuantity types.Quantity `db:"total_shortage_quantity" json:"totalShortageQuantity"`

	Lines []InventoryLine `db:"-" json:"lines"`
}

// InventoryLine represents a line in the inventory.
type InventoryLine struct {
	LineID    id.ID `db:"line_id" json:"lineId"`
	LineNo    int   `db:"line_no" json:"lineNo"`
	ProductID id.ID `db:"product_id" json:"productId"`

	BookQuantity   types.Quantity  `db:"book_quantity" json:"bookQuantity"`
	ActualQuantity *types.Quantity `db:"actual_quantity" json:"actualQuantity,omitempty"`
	Deviation      types.Quantity  `db:"deviation" json:"deviation"`

	UnitPrice       types.MinorUnits `db:"unit_price" json:"unitPrice"`
	DeviationAmount types.MinorUnits `db:"deviation_amount" json:"deviationAmount"`

	Counted   bool       `db:"counted" json:"counted"`
	CountedAt *time.Time `db:"counted_at" json:"countedAt,omitempty"`
	CountedBy *string    `db:"counted_by" json:"countedBy,omitempty"`
}

// NewInventory creates a new inventory document.
func NewInventory(organizationID id.ID, warehouseID id.ID) *Inventory {
	return &Inventory{
		Document:    entity.NewDocument(organizationID),
		WarehouseID: warehouseID,
		Status:      StatusDraft,
		StartDate:   time.Now().UTC(),
		Lines:       make([]InventoryLine, 0),
	}
}

// AddLine adds a line to the inventory.
func (inv *Inventory) AddLine(productID id.ID, bookQuantity types.Quantity, unitPrice types.MinorUnits) {
	lineNo := len(inv.Lines) + 1

	line := InventoryLine{
		LineID:       id.New(),
		LineNo:       lineNo,
		ProductID:    productID,
		BookQuantity: bookQuantity,
		UnitPrice:    unitPrice,
		Counted:      false,
	}

	inv.Lines = append(inv.Lines, line)
	inv.recalculateTotals()
}

// SetActualQuantity sets the actual quantity for a line.
func (inv *Inventory) SetActualQuantity(lineNo int, actualQty types.Quantity, countedBy string) error {
	if lineNo < 1 || lineNo > len(inv.Lines) {
		return apperror.NewValidation("invalid line number")
	}

	idx := lineNo - 1
	inv.Lines[idx].ActualQuantity = &actualQty
	inv.Lines[idx].Deviation = actualQty - inv.Lines[idx].BookQuantity
	// Deviation Amount calculation using integer arithmetic: (DeviationScaled * UnitPrice) / 10000
	inv.Lines[idx].DeviationAmount = types.MinorUnits((inv.Lines[idx].Deviation.Int64Scaled() * int64(inv.Lines[idx].UnitPrice)) / 10000)
	inv.Lines[idx].Counted = true
	now := time.Now().UTC()
	inv.Lines[idx].CountedAt = &now
	inv.Lines[idx].CountedBy = &countedBy

	inv.recalculateTotals()
	return nil
}

func (inv *Inventory) recalculateTotals() {
	inv.TotalBookQuantity = types.Quantity(0)
	inv.TotalActualQuantity = types.Quantity(0)
	inv.TotalSurplusQuantity = types.Quantity(0)
	inv.TotalShortageQuantity = types.Quantity(0)

	for _, line := range inv.Lines {
		inv.TotalBookQuantity += line.BookQuantity
		if line.ActualQuantity != nil {
			inv.TotalActualQuantity += *line.ActualQuantity
			if line.Deviation > 0 {
				inv.TotalSurplusQuantity += line.Deviation
			} else if line.Deviation < 0 {
				inv.TotalShortageQuantity += -line.Deviation
			}
		}
	}
}

// Validate implements entity.Validatable.
func (inv *Inventory) Validate(ctx context.Context) error {
	if err := inv.Document.Validate(ctx); err != nil {
		return err
	}

	if id.IsNil(inv.WarehouseID) {
		return apperror.NewValidation("warehouse is required").
			WithDetail("field", "warehouseId")
	}

	if inv.StartDate.IsZero() {
		return apperror.NewValidation("start date is required").
			WithDetail("field", "startDate")
	}

	return nil
}

// CanPost validates if inventory can be posted.
func (inv *Inventory) CanPost(ctx context.Context) error {
	if err := inv.Validate(ctx); err != nil {
		return err
	}

	if inv.Status != StatusCompleted {
		return apperror.NewBusinessRule(
			"INVENTORY_NOT_COMPLETED",
			"Inventory must be completed before posting",
		)
	}

	// Check all lines are counted
	for i, line := range inv.Lines {
		if !line.Counted {
			return apperror.NewBusinessRule(
				"LINE_NOT_COUNTED",
				"All lines must be counted before posting",
			).WithDetail("lineNo", i+1)
		}
	}

	return nil
}

// Start transitions inventory to in_progress status.
func (inv *Inventory) Start() error {
	if inv.Status != StatusDraft {
		return apperror.NewBusinessRule("INVALID_STATUS", "Can only start from draft status")
	}
	inv.Status = StatusInProgress
	return nil
}

// Complete transitions inventory to completed status.
func (inv *Inventory) Complete() error {
	if inv.Status != StatusInProgress {
		return apperror.NewBusinessRule("INVALID_STATUS", "Can only complete from in_progress status")
	}

	// Check all lines counted
	for i, line := range inv.Lines {
		if !line.Counted {
			return apperror.NewBusinessRule(
				"LINE_NOT_COUNTED",
				"All lines must be counted before completing",
			).WithDetail("lineNo", i+1)
		}
	}

	inv.Status = StatusCompleted
	now := time.Now().UTC()
	inv.EndDate = &now
	return nil
}

// Cancel transitions inventory to cancelled status.
func (inv *Inventory) Cancel() error {
	if inv.Status == StatusCompleted || inv.Posted {
		return apperror.NewBusinessRule("CANNOT_CANCEL", "Cannot cancel completed or posted inventory")
	}
	inv.Status = StatusCancelled
	return nil
}

// --- Postable interface implementation ---
// GetID, GetPostedVersion, IsPosted, MarkPosted, MarkUnposted are inherited from entity.Document
// Note: Inventory overrides CanPost with custom logic (lines 146-170)

func (inv *Inventory) GetDocumentType() string { return "Inventory" }

// GenerateMovements creates register movements for deviations.
// Surplus = Receipt, Shortage = Expense
func (inv *Inventory) GenerateMovements(ctx context.Context) (*posting.MovementSet, error) {
	movements := posting.NewMovementSet()
	newVersion := inv.PostedVersion + 1

	for _, line := range inv.Lines {
		if line.Deviation == 0 {
			continue
		}

		var recordType entity.RecordType
		var qty types.Quantity

		if line.Deviation > 0 {
			// Surplus: receipt
			recordType = entity.RecordTypeReceipt
			qty = line.Deviation
		} else {
			// Shortage: expense
			recordType = entity.RecordTypeExpense
			qty = -line.Deviation
		}

		stockMovement := entity.NewStockMovement(
			inv.ID,
			inv.GetDocumentType(),
			newVersion,
			inv.Date,
			recordType,
			inv.WarehouseID,
			line.ProductID,
			qty,
		)

		movements.AddStock(stockMovement)
	}

	return movements, nil
}

var _ posting.Postable = (*Inventory)(nil)
