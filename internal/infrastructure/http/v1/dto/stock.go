package dto

import (
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/registers/stock"
)

// --- Response DTOs for Stock Register ---

// StockBalanceResponse represents stock balance in API responses.
type StockBalanceResponse struct {
	WarehouseID    string     `json:"warehouseId"`
	NomenclatureID      string     `json:"nomenclatureId"`
	Quantity       float64    `json:"quantity"`
	LastMovementAt *time.Time `json:"lastMovementAt,omitempty"`
}

// FromStockBalance converts entity to response DTO.
func FromStockBalance(b entity.StockBalance) StockBalanceResponse {

	// Convert zero-value time.Time (Domain) to nil pointer (DTO/JSON),
	// so that the API returns null or omits the field instead of "0001-01-01".
	var lastMovement *time.Time
	if !b.LastMovementAt.IsZero() {
		// Copy value to avoid pointer issues if "b" is reused
		val := b.LastMovementAt
		lastMovement = &val
	}

	return StockBalanceResponse{
		WarehouseID:    b.WarehouseID.String(),
		NomenclatureID:      b.NomenclatureID.String(),
		Quantity:       b.Quantity.Float64(),
		LastMovementAt: lastMovement,
	}
}

// StockMovementResponse represents stock movement in API responses.
type StockMovementResponse struct {
	LineID          string    `json:"lineId"`
	RecorderID      string    `json:"recorderId"`
	RecorderType    string    `json:"recorderType"`
	RecorderVersion int       `json:"recorderVersion"`
	Period          time.Time `json:"period"`
	RecordType      string    `json:"recordType"`
	WarehouseID     string    `json:"warehouseId"`
	NomenclatureID       string    `json:"nomenclatureId"`
	Quantity        float64   `json:"quantity"`
	CreatedAt       time.Time `json:"createdAt"`
}

// FromStockMovement converts entity to response DTO.
func FromStockMovement(m entity.StockMovement) StockMovementResponse {
	resp := StockMovementResponse{
		LineID:          m.LineID.String(),
		RecorderID:      m.RecorderID.String(),
		RecorderType:    m.RecorderType,
		RecorderVersion: m.RecorderVersion,
		Period:          m.Period,
		RecordType:      string(m.RecordType),
		WarehouseID:     m.WarehouseID.String(),
		NomenclatureID:       m.NomenclatureID.String(),
		Quantity:        m.Quantity.Float64(),
		CreatedAt:       m.CreatedAt,
	}

	return resp
}

// StockTurnoverResponse represents stock turnover report.
type StockTurnoverResponse struct {
	WarehouseID    string  `json:"warehouseId,omitempty"`
	NomenclatureID      string  `json:"nomenclatureId,omitempty"`
	OpeningBalance float64 `json:"openingBalance"`
	Receipt        float64 `json:"receipt"`
	Expense        float64 `json:"expense"`
	ClosingBalance float64 `json:"closingBalance"`
}

// FromStockTurnover converts domain turnover to response DTO.
func FromStockTurnover(t stock.Turnover) StockTurnoverResponse {
	resp := StockTurnoverResponse{
		OpeningBalance: t.OpeningBalance.Float64(),
		Receipt:        t.Receipt.Float64(),
		Expense:        t.Expense.Float64(),
		ClosingBalance: t.ClosingBalance.Float64(),
	}
	if !id.IsNil(t.WarehouseID) {
		resp.WarehouseID = t.WarehouseID.String()
	}
	if !id.IsNil(t.NomenclatureID) {
		resp.NomenclatureID = t.NomenclatureID.String()
	}
	return resp
}

// StockBalanceListResponse represents a list of stock balances.
type StockBalanceListResponse struct {
	Items []StockBalanceResponse `json:"items"`
}

// StockMovementListResponse represents a list of stock movements.
type StockMovementListResponse struct {
	Items      []StockMovementResponse `json:"items"`
	TotalCount int                     `json:"totalCount,omitempty"`
}
