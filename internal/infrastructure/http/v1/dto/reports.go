package dto

import (
	"time"

	"metapus/internal/domain/reports"
)

// --- Stock Balance Report ---

// StockBalanceReportRequest represents request for stock balance report.
type StockBalanceReportRequest struct {
	AsOfDate     *time.Time `form:"asOfDate"`
	WarehouseIDs []string   `form:"warehouseId"`
	ProductIDs   []string   `form:"productId"`
	ExcludeZero  *bool      `form:"excludeZero"`
	Limit        int        `form:"limit"`
	Offset       int        `form:"offset"`
}

// StockBalanceReportResponse represents stock balance report response.
type StockBalanceReportResponse struct {
	AsOfDate      string                           `json:"asOfDate"`
	Items         []StockBalanceReportItemResponse `json:"items"`
	TotalItems    int                              `json:"totalItems"`
	TotalQuantity float64                          `json:"totalQuantity"`
}

// StockBalanceReportItemResponse represents a single item in stock balance report.
type StockBalanceReportItemResponse struct {
	WarehouseID   string  `json:"warehouseId"`
	WarehouseName string  `json:"warehouseName"`
	ProductID     string  `json:"productId"`
	ProductName   string  `json:"productName"`
	ProductSKU    string  `json:"productSku,omitempty"`
	UnitName      string  `json:"unitName,omitempty"`
	Quantity      float64 `json:"quantity"`
}

// FromStockBalanceReport converts domain report to response DTO.
func FromStockBalanceReport(r *reports.StockBalanceReport) *StockBalanceReportResponse {
	resp := &StockBalanceReportResponse{
		AsOfDate:      r.AsOfDate.Format(time.RFC3339),
		Items:         make([]StockBalanceReportItemResponse, len(r.Items)),
		TotalItems:    r.TotalItems,
		TotalQuantity: r.TotalQuantity,
	}

	for i, item := range r.Items {
		resp.Items[i] = StockBalanceReportItemResponse{
			WarehouseID:   item.WarehouseID.String(),
			WarehouseName: item.WarehouseName,
			ProductID:     item.ProductID.String(),
			ProductName:   item.ProductName,
			ProductSKU:    item.ProductSKU,
			UnitName:      item.UnitName,
			Quantity:      item.Quantity,
		}
	}

	return resp
}

// --- Stock Turnover Report ---

// StockTurnoverReportRequest represents request for stock turnover report.
type StockTurnoverReportRequest struct {
	FromDate     string   `form:"fromDate" binding:"required"`
	ToDate       string   `form:"toDate" binding:"required"`
	WarehouseIDs []string `form:"warehouseId"`
	ProductIDs   []string `form:"productId"`
	IncludeZero  bool     `form:"includeZero"`
	Limit        int      `form:"limit"`
	Offset       int      `form:"offset"`
}

// StockTurnoverReportResponse represents stock turnover report response.
type StockTurnoverReportResponse struct {
	FromDate     string                            `json:"fromDate"`
	ToDate       string                            `json:"toDate"`
	Items        []StockTurnoverReportItemResponse `json:"items"`
	TotalItems   int                               `json:"totalItems"`
	TotalOpening float64                           `json:"totalOpening"`
	TotalReceipt float64                           `json:"totalReceipt"`
	TotalExpense float64                           `json:"totalExpense"`
	TotalClosing float64                           `json:"totalClosing"`
}

// StockTurnoverReportItemResponse represents a single item in turnover report.
type StockTurnoverReportItemResponse struct {
	WarehouseID    string  `json:"warehouseId,omitempty"`
	WarehouseName  string  `json:"warehouseName,omitempty"`
	ProductID      string  `json:"productId,omitempty"`
	ProductName    string  `json:"productName,omitempty"`
	ProductSKU     string  `json:"productSku,omitempty"`
	UnitName       string  `json:"unitName,omitempty"`
	OpeningBalance float64 `json:"openingBalance"`
	Receipt        float64 `json:"receipt"`
	Expense        float64 `json:"expense"`
	ClosingBalance float64 `json:"closingBalance"`
}

// FromStockTurnoverReport converts domain report to response DTO.
func FromStockTurnoverReport(r *reports.StockTurnoverReport) *StockTurnoverReportResponse {
	resp := &StockTurnoverReportResponse{
		FromDate:     r.FromDate.Format(time.RFC3339),
		ToDate:       r.ToDate.Format(time.RFC3339),
		Items:        make([]StockTurnoverReportItemResponse, len(r.Items)),
		TotalItems:   r.TotalItems,
		TotalOpening: r.TotalOpening,
		TotalReceipt: r.TotalReceipt,
		TotalExpense: r.TotalExpense,
		TotalClosing: r.TotalClosing,
	}

	for i, item := range r.Items {
		resp.Items[i] = StockTurnoverReportItemResponse{
			WarehouseID:    item.WarehouseID.String(),
			WarehouseName:  item.WarehouseName,
			ProductID:      item.ProductID.String(),
			ProductName:    item.ProductName,
			ProductSKU:     item.ProductSKU,
			UnitName:       item.UnitName,
			OpeningBalance: item.OpeningBalance,
			Receipt:        item.Receipt,
			Expense:        item.Expense,
			ClosingBalance: item.ClosingBalance,
		}
	}

	return resp
}

// --- Document Journal ---

// DocumentJournalRequest represents request for document journal.
type DocumentJournalRequest struct {
	FromDate       *string  `form:"fromDate"`
	ToDate         *string  `form:"toDate"`
	DocumentTypes  []string `form:"documentType"`
	Posted         *bool    `form:"posted"`
	NumberContains string   `form:"number"`
	WarehouseIDs   []string `form:"warehouseId"`
	SupplierIDs    []string `form:"supplierId"`
	SortBy         string   `form:"sortBy"`
	SortOrder      string   `form:"sortOrder"`
	Limit          int      `form:"limit"`
	Offset         int      `form:"offset"`
}

// DocumentJournalResponse represents document journal response.
type DocumentJournalResponse struct {
	Items      []DocumentJournalItemResponse `json:"items"`
	TotalCount int                           `json:"totalCount"`
	Limit      int                           `json:"limit"`
	Offset     int                           `json:"offset"`
	Summary    []DocumentTypeSummaryResponse `json:"summary,omitempty"`
}

// DocumentJournalItemResponse represents a document in journal.
type DocumentJournalItemResponse struct {
	ID               string  `json:"id"`
	DocumentType     string  `json:"documentType"`
	Number           string  `json:"number"`
	Date             string  `json:"date"`
	Posted           bool    `json:"posted"`
	CounterpartyID   *string `json:"counterpartyId,omitempty"`
	CounterpartyName string  `json:"counterpartyName,omitempty"`
	WarehouseID      *string `json:"warehouseId,omitempty"`
	WarehouseName    string  `json:"warehouseName,omitempty"`
	TotalQuantity    float64 `json:"totalQuantity"`
	TotalAmount      int64   `json:"totalAmount"`
	Currency         string  `json:"currency"`
	Description      string  `json:"description,omitempty"`
	DeletionMark     bool    `json:"deletionMark,omitempty"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

// DocumentTypeSummaryResponse represents summary by document type.
type DocumentTypeSummaryResponse struct {
	DocumentType  string  `json:"documentType"`
	Count         int     `json:"count"`
	PostedCount   int     `json:"postedCount"`
	TotalQuantity float64 `json:"totalQuantity"`
	TotalAmount   int64   `json:"totalAmount"`
}

// FromDocumentJournal converts domain journal to response DTO.
func FromDocumentJournal(j *reports.DocumentJournal) *DocumentJournalResponse {
	resp := &DocumentJournalResponse{
		Items:      make([]DocumentJournalItemResponse, len(j.Items)),
		TotalCount: j.TotalCount,
		Limit:      j.Limit,
		Offset:     j.Offset,
	}

	for i, item := range j.Items {
		resp.Items[i] = DocumentJournalItemResponse{
			ID:               item.ID.String(),
			DocumentType:     item.DocumentType,
			Number:           item.Number,
			Date:             item.Date.Format(time.RFC3339),
			Posted:           item.Posted,
			CounterpartyName: item.CounterpartyName,
			WarehouseName:    item.WarehouseName,
			TotalQuantity:    item.TotalQuantity,
			TotalAmount:      item.TotalAmount,
			Currency:         item.Currency,
			Description:      item.Description,
			DeletionMark:     item.DeletionMark,
			CreatedAt:        item.CreatedAt.Format(time.RFC3339),
			UpdatedAt:        item.UpdatedAt.Format(time.RFC3339),
		}

		if item.CounterpartyID != nil {
			s := item.CounterpartyID.String()
			resp.Items[i].CounterpartyID = &s
		}
		if item.WarehouseID != nil {
			s := item.WarehouseID.String()
			resp.Items[i].WarehouseID = &s
		}
	}

	if j.Summary != nil {
		resp.Summary = make([]DocumentTypeSummaryResponse, len(j.Summary))
		for i, s := range j.Summary {
			resp.Summary[i] = DocumentTypeSummaryResponse{
				DocumentType:  s.DocumentType,
				Count:         s.Count,
				PostedCount:   s.PostedCount,
				TotalQuantity: s.TotalQuantity,
				TotalAmount:   s.TotalAmount,
			}
		}
	}

	return resp
}
