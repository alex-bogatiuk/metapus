// Package entity provides core domain entities.
package entity

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// RecordType defines movement direction for accumulation registers.
type RecordType string

const (
	// RecordTypeReceipt increases balance (приход)
	RecordTypeReceipt RecordType = "receipt"
	// RecordTypeExpense decreases balance (расход)
	RecordTypeExpense RecordType = "expense"
)

// RegisterKind defines the type of register.
type RegisterKind string

const (
	// RegisterKindAccumulation - tracks quantities and amounts (Регистр накопления)
	RegisterKindAccumulation RegisterKind = "accumulation"
	// RegisterKindInformation - stores dimensional data (Регистр сведений)
	RegisterKindInformation RegisterKind = "information"
)

// MovementBase contains common fields for all register movements.
// Movements are immutable - they are never updated, only deleted and recreated.
type MovementBase struct {
	// LineID is unique identifier for this movement line (UUIDv7)
	// Used instead of hash for deterministic tracking
	LineID id.ID `db:"line_id" json:"lineId"`

	// RecorderID is the document that created this movement
	RecorderID id.ID `db:"recorder_id" json:"recorderId"`

	// RecorderType is the document type (e.g., "GoodsReceipt", "Invoice")
	RecorderType string `db:"recorder_type" json:"recorderType"`

	// RecorderVersion tracks which posting iteration created this movement
	// Allows efficient cleanup: DELETE WHERE recorder_id = X AND recorder_version < Y
	RecorderVersion int `db:"recorder_version" json:"recorderVersion"`

	// Period is the business date for the movement (for period-based queries)
	Period time.Time `db:"period" json:"period"`

	// RecordType: receipt (приход) or expense (расход)
	RecordType RecordType `db:"record_type" json:"recordType"`

	// CreatedAt is when the movement was recorded
	CreatedAt time.Time `db:"created_at" json:"createdAt"`
}

// NewMovementBase creates a new movement base with generated LineID.
func NewMovementBase(recorderID id.ID, recorderType string, recorderVersion int, period time.Time, recordType RecordType) MovementBase {
	return MovementBase{
		LineID:          id.New(),
		RecorderID:      recorderID,
		RecorderType:    recorderType,
		RecorderVersion: recorderVersion,
		Period:          period,
		RecordType:      recordType,
		CreatedAt:       time.Now().UTC(),
	}
}

// StockMovement represents a movement in the stock accumulation register.
// Tracks quantity changes for products in warehouses.
type StockMovement struct {
	MovementBase

	// Dimensions (измерения)
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId"`
	ProductID   id.ID `db:"product_id" json:"productId"`

	// Resources (ресурсы)
	Quantity types.Quantity `db:"quantity" json:"quantity"`
}

// NewStockMovement creates a new stock movement.
func NewStockMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType RecordType,
	warehouseID, productID id.ID,
	quantity types.Quantity,
) StockMovement {
	return StockMovement{
		MovementBase: NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		WarehouseID:  warehouseID,
		ProductID:    productID,
		Quantity:     quantity,
	}
}

// SignedQuantity returns quantity with sign based on record type.
// Receipt = positive, Expense = negative.
func (m *StockMovement) SignedQuantity() types.Quantity {
	if m.RecordType == RecordTypeExpense {
		return m.Quantity.Neg()
	}
	return m.Quantity
}

// StockBalance represents current balance in the stock register.
// This is a materialized/cached view for fast balance queries.
type StockBalance struct {
	// Dimensions
	WarehouseID id.ID  `db:"warehouse_id" json:"warehouseId"`
	ProductID   id.ID  `db:"product_id" json:"productId"`

	// Balances
	Quantity types.Quantity `db:"quantity" json:"quantity"`

	// Metadata
	LastMovementAt time.Time `db:"last_movement_at" json:"lastMovementAt"`
	UpdatedAt      time.Time `db:"updated_at" json:"updatedAt"`
}

// NOTE: CostMovement and SettlementMovement were removed (YAGNI).
// These structures should be added when actual implementation is needed.
