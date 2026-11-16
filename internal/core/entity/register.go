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
// Crypto movements are append-only facts; corrections append linked opposites.
type MovementBase struct {
	// LineID is unique identifier for this movement line (UUIDv7)
	// Assigned by the persistence adapter; generation returns an unstamped plan.
	LineID id.ID `db:"line_id" json:"lineId"`

	// RecorderID is the document that created this movement
	RecorderID id.ID `db:"recorder_id" json:"recorderId"`

	// RecorderType is the document type (e.g., "CryptoInvoice", "CryptoPayment")
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

// NewMovementBase creates a deterministic business plan. Identity and recording
// time are deliberately assigned only at the persistence boundary.
func NewMovementBase(recorderID id.ID, recorderType string, recorderVersion int, period time.Time, recordType RecordType) MovementBase {
	return MovementBase{
		RecorderID:      recorderID,
		RecorderType:    recorderType,
		RecorderVersion: recorderVersion,
		Period:          period,
		RecordType:      recordType,
	}
}

// SetRecordingMetadata accepts explicit adapter-supplied metadata. It preserves
// existing identities when importing or recording an already prepared fact.
func (m *MovementBase) SetRecordingMetadata(lineID id.ID, at time.Time) {
	if id.IsNil(m.LineID) {
		m.LineID = lineID
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = at
	}
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
	PolicyEntity string              `json:"-"` // Explicit logical register policy key.
	RegisterName string              `json:"registerName"`
	RecordType   string              `json:"recordType"` // "receipt" or "expense"
	Period       time.Time           `json:"period"`
	Columns      []MovementColumnDef `json:"columns"` // Metadata: label + type per field
	Data         map[string]any      `json:"data"`    // Dynamic fields — enriched values
}

// MovementColumnDef describes a single column in the movements table.
// Used by the frontend to render sortable headers with correct labels.
type MovementColumnDef struct {
	CurrencyRef string `json:"currencyRef,omitempty"`
	PolicyField string `json:"-"`     // Explicit register field, independent of UI key.
	Key         string `json:"key"`   // Field key in Data map, e.g. "product"
	Label       string `json:"label"` // Human-readable, e.g. "Товар"
	Type        string `json:"type"`  // "ref" | "amount" | "quantity" | "text"
	// RefEntity names the metadata entity a "ref" column points to, e.g.
	// "Counterparty". Readers resolve the reference and its link through it;
	// a ref column without it is not shown.
	RefEntity string `json:"-"`
}

// MovementRefValue is the enriched value for ref-type columns.
// Frontend renders it as a clickable link with human-readable name.
type MovementRefValue struct {
	CurrencyCode  string `json:"currencyCode,omitempty"`
	DecimalPlaces *int   `json:"decimalPlaces,omitempty"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url,omitempty"` // e.g. "/catalogs/tokens/019d..."
}

// MovementProvider defines an interface for any register service that can
// return its movements for a specific document (recorder).
type MovementProvider interface {
	// RegisterName returns the human-readable name of the register.
	RegisterName() string

	// GetDocumentMovements fetched all movements for the specified recorder ID, mapped to the generic type.
	GetDocumentMovements(ctx context.Context, recorderID id.ID) ([]DocumentMovement, error)
}
