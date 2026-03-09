// Package reports provides report generation services.
package reports

import (
	"time"

	"metapus/internal/core/id"
)

// --- Stock Balance Report ---

// StockBalanceReportFilter defines filter for stock balance report.
type StockBalanceReportFilter struct {
	// AsOfDate - report date (defaults to now)
	AsOfDate *time.Time

	// Filters
	WarehouseIDs []id.ID
	ProductIDs   []id.ID

	// Grouping options
	GroupByWarehouse bool
	GroupByProduct   bool

	// Exclude zero balances
	ExcludeZero bool

	// Pagination
	Limit  int
	Offset int
}

// StockBalanceReportItem represents a single row in stock balance report.
type StockBalanceReportItem struct {
	WarehouseID   id.ID   `json:"warehouseId"`
	WarehouseName string  `json:"warehouseName"`
	ProductID     id.ID   `json:"productId"`
	ProductName   string  `json:"productName"`
	ProductSKU    string  `json:"productSku"`
	UnitName      string  `json:"unitName"`
	Quantity      float64 `json:"quantity"`
	// Calculated cost (if cost accounting is enabled)
	TotalCost int64 `json:"totalCost,omitempty"`
}

// StockBalanceReport represents the full stock balance report.
type StockBalanceReport struct {
	AsOfDate   time.Time                `json:"asOfDate"`
	Items      []StockBalanceReportItem `json:"items"`
	TotalItems int                      `json:"totalItems"`

	// Summary
	TotalQuantity float64 `json:"totalQuantity"`
	TotalCost     int64   `json:"totalCost,omitempty"`
}

// --- Stock Turnover Report ---

// StockTurnoverReportFilter defines filter for stock turnover report.
type StockTurnoverReportFilter struct {
	// Period (required)
	FromDate time.Time
	ToDate   time.Time

	// Filters
	WarehouseIDs []id.ID
	ProductIDs   []id.ID

	// Grouping
	GroupByWarehouse bool
	GroupByProduct   bool

	// Include zero rows
	IncludeZero bool

	// Pagination
	Limit  int
	Offset int
}

// StockTurnoverReportItem represents a single row in turnover report.
type StockTurnoverReportItem struct {
	WarehouseID    id.ID   `json:"warehouseId,omitempty"`
	WarehouseName  string  `json:"warehouseName,omitempty"`
	ProductID      id.ID   `json:"productId,omitempty"`
	ProductName    string  `json:"productName,omitempty"`
	ProductSKU     string  `json:"productSku,omitempty"`
	UnitName       string  `json:"unitName,omitempty"`
	OpeningBalance float64 `json:"openingBalance"`
	Receipt        float64 `json:"receipt"`
	Expense        float64 `json:"expense"`
	ClosingBalance float64 `json:"closingBalance"`
}

// StockTurnoverReport represents the full turnover report.
type StockTurnoverReport struct {
	FromDate   time.Time                 `json:"fromDate"`
	ToDate     time.Time                 `json:"toDate"`
	Items      []StockTurnoverReportItem `json:"items"`
	TotalItems int                       `json:"totalItems"`

	// Summary totals
	TotalOpening float64 `json:"totalOpening"`
	TotalReceipt float64 `json:"totalReceipt"`
	TotalExpense float64 `json:"totalExpense"`
	TotalClosing float64 `json:"totalClosing"`
}

// --- Document Journal ---

// DocumentJournalFilter defines filter for document journal.
type DocumentJournalFilter struct {
	// Period
	FromDate *time.Time
	ToDate   *time.Time

	// Document types filter
	DocumentTypes []string

	// Status filter
	Posted     *bool
	HasChanges *bool

	// Search by number
	NumberContains string

	// Filters by references
	WarehouseIDs []id.ID
	SupplierIDs  []id.ID
	CustomerIDs  []id.ID

	// Sorting
	SortBy    string // "date", "number", "type", "amount"
	SortOrder string // "asc", "desc"

	// Pagination
	Limit  int
	Offset int
}

// DocumentJournalItem represents a document in the journal.
type DocumentJournalItem struct {
	ID           id.ID     `json:"id"`
	DocumentType string    `json:"documentType"`
	Number       string    `json:"number"`
	Date         time.Time `json:"date"`
	Posted       bool      `json:"posted"`

	// Counterparty info
	CounterpartyID   *id.ID `json:"counterpartyId,omitempty"`
	CounterpartyName string `json:"counterpartyName,omitempty"`

	// Warehouse info
	WarehouseID   *id.ID `json:"warehouseId,omitempty"`
	WarehouseName string `json:"warehouseName,omitempty"`

	// Amounts
	TotalQuantity float64 `json:"totalQuantity"`
	TotalAmount   int64   `json:"totalAmount"`
	Currency      string  `json:"currency"`

	Description  string    `json:"description,omitempty"`
	DeletionMark bool      `json:"deletionMark"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// DocumentJournal represents the document journal result.
type DocumentJournal struct {
	Items      []DocumentJournalItem `json:"items"`
	TotalCount int                   `json:"totalCount"`
	Limit      int                   `json:"limit"`
	Offset     int                   `json:"offset"`

	// Summary by document type
	Summary []DocumentTypeSummary `json:"summary,omitempty"`
}

// DocumentTypeSummary provides count and totals by document type.
type DocumentTypeSummary struct {
	DocumentType  string  `json:"documentType"`
	Count         int     `json:"count"`
	PostedCount   int     `json:"postedCount"`
	TotalQuantity float64 `json:"totalQuantity"`
	TotalAmount   int64   `json:"totalAmount"`
}
