// Package entity provides core domain entities.
package entity

import (
	"context"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// RecordType defines movement direction for accumulation registers.
type RecordType string

const (
	// RecordTypeReceipt increases balance (receipt)
	RecordTypeReceipt RecordType = "receipt"
	// RecordTypeExpense decreases balance (expense)
	RecordTypeExpense RecordType = "expense"
)

// RegisterKind defines the type of register.
type RegisterKind string

const (
	// RegisterKindAccumulation - tracks quantities and amounts (Accumulation Register)
	RegisterKindAccumulation RegisterKind = "accumulation"
	// RegisterKindInformation - stores dimensional data (Information Register)
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

	// RecordType: receipt or expense
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

	// Dimensions
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId"`
	ProductID   id.ID `db:"product_id" json:"productId"`

	// Resources
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
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId"`
	ProductID   id.ID `db:"product_id" json:"productId"`

	// Balances
	Quantity types.Quantity `db:"quantity" json:"quantity"`

	// Metadata
	LastMovementAt time.Time `db:"last_movement_at" json:"lastMovementAt"`
	UpdatedAt      time.Time `db:"updated_at" json:"updatedAt"`
}

// ---------------------------------------------------------------------------
// Cost accumulation register (Stock Cost Register)
// ---------------------------------------------------------------------------

// CostMovement represents a movement in the cost accumulation register.
// Tracks quantity and monetary amount changes for products in warehouses.
type CostMovement struct {
	MovementBase

	// Dimensions
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId"`
	ProductID   id.ID `db:"product_id" json:"productId"`
	CurrencyID  id.ID `db:"currency_id" json:"currencyId"`

	// Resources
	Quantity types.Quantity   `db:"quantity" json:"quantity"`
	Amount   types.MinorUnits `db:"amount" json:"amount"`
}

// NewCostMovement creates a new cost movement.
func NewCostMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType RecordType,
	warehouseID, productID, currencyID id.ID,
	quantity types.Quantity,
	amount types.MinorUnits,
) CostMovement {
	return CostMovement{
		MovementBase: NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		WarehouseID:  warehouseID,
		ProductID:    productID,
		CurrencyID:   currencyID,
		Quantity:     quantity,
		Amount:       amount,
	}
}

// SignedAmount returns amount with sign based on record type.
func (m *CostMovement) SignedAmount() types.MinorUnits {
	if m.RecordType == RecordTypeExpense {
		return m.Amount.Neg()
	}
	return m.Amount
}

// CostBalance represents current balance in the cost register.
type CostBalance struct {
	// Dimensions
	WarehouseID id.ID `db:"warehouse_id" json:"warehouseId"`
	ProductID   id.ID `db:"product_id" json:"productId"`
	CurrencyID  id.ID `db:"currency_id" json:"currencyId"`

	// Balances
	Quantity types.Quantity   `db:"quantity" json:"quantity"`
	Amount   types.MinorUnits `db:"amount" json:"amount"`

	// Metadata
	LastMovementAt time.Time `db:"last_movement_at" json:"lastMovementAt"`
	UpdatedAt      time.Time `db:"updated_at" json:"updatedAt"`
}

// ---------------------------------------------------------------------------
// Settlement accumulation register (Settlements Register)
// ---------------------------------------------------------------------------

// SettlementMovement represents a movement in the settlement accumulation register.
// Tracks monetary amounts for counterparty settlements (receivables/payables).
type SettlementMovement struct {
	MovementBase

	// Dimensions
	CounterpartyID id.ID  `db:"counterparty_id" json:"counterpartyId"`
	ContractID     *id.ID `db:"contract_id" json:"contractId"`
	CurrencyID     id.ID  `db:"currency_id" json:"currencyId"`

	// Resources
	Amount types.MinorUnits `db:"amount" json:"amount"`
}

// NewSettlementMovement creates a new settlement movement.
func NewSettlementMovement(
	recorderID id.ID,
	recorderType string,
	recorderVersion int,
	period time.Time,
	recordType RecordType,
	counterpartyID id.ID,
	contractID *id.ID,
	currencyID id.ID,
	amount types.MinorUnits,
) SettlementMovement {
	return SettlementMovement{
		MovementBase:   NewMovementBase(recorderID, recorderType, recorderVersion, period, recordType),
		CounterpartyID: counterpartyID,
		ContractID:     contractID,
		CurrencyID:     currencyID,
		Amount:         amount,
	}
}

// SignedAmount returns amount with sign based on record type.
func (m *SettlementMovement) SignedAmount() types.MinorUnits {
	if m.RecordType == RecordTypeExpense {
		return m.Amount.Neg()
	}
	return m.Amount
}

// SettlementBalance represents current balance in the settlement register.
type SettlementBalance struct {
	// Dimensions
	CounterpartyID id.ID  `db:"counterparty_id" json:"counterpartyId"`
	ContractID     *id.ID `db:"contract_id" json:"contractId"`
	CurrencyID     id.ID  `db:"currency_id" json:"currencyId"`

	// Balances
	Amount types.MinorUnits `db:"amount" json:"amount"`

	// Metadata
	LastMovementAt time.Time `db:"last_movement_at" json:"lastMovementAt"`
	UpdatedAt      time.Time `db:"updated_at" json:"updatedAt"`
}

// ---------------------------------------------------------------------------
// Generic Document Movements (Cross-Register Abstraction)
// ---------------------------------------------------------------------------

// DocumentMovement represents a generalized register movement format for UI consumption.
// It abstracts away the specific table fields into a dynamic Data map.
//
// Columns provides metadata-driven rendering hints (label, type) for each data key,
// allowing the frontend to display human-readable headers and format values correctly.
//
// Data values for ref-type columns are MovementRefValue objects: {id, name, url}.
// Data values for amount-type columns are raw MinorUnits (int64).
// Data values for quantity-type columns are float64.
type DocumentMovement struct {
	RegisterName string                 `json:"registerName"`
	RecordType   string                 `json:"recordType"` // "receipt" or "expense"
	Period       time.Time              `json:"period"`
	Columns      []MovementColumnDef    `json:"columns"`    // Metadata: label + type per field
	Data         map[string]interface{} `json:"data"`       // Dynamic fields — enriched values
}

// MovementColumnDef describes a single column in the movements table.
// Used by the frontend to render sortable headers with correct labels.
type MovementColumnDef struct {
	Key   string `json:"key"`   // Field key in Data map, e.g. "product"
	Label string `json:"label"` // Human-readable, e.g. "Товар"
	Type  string `json:"type"`  // "ref" | "amount" | "quantity" | "text"
}

// MovementRefValue is the enriched value for ref-type columns.
// Frontend renders it as a clickable link with human-readable name.
type MovementRefValue struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url,omitempty"` // e.g. "/catalogs/products/019d..."
}

// MovementProvider defines an interface for any register service that can
// return its movements for a specific document (recorder).
type MovementProvider interface {
	// RegisterName returns the human-readable name of the register.
	RegisterName() string

	// GetDocumentMovements fetched all movements for the specified recorder ID, mapped to the generic type.
	GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]DocumentMovement, error)
}
