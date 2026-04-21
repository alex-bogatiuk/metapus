package reports

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/platform"
)

// ---------------------------------------------------------------------------
// Stock Balance Report (typed contract)
// ---------------------------------------------------------------------------

// StockBalanceExecutor implements platform.ReportRegistration[StockBalanceFilter, *StockBalanceReport].
// Replaces the old manual handler/service/dto chain with a single struct.
type StockBalanceExecutor struct {
	repo Repository
}

func NewStockBalanceExecutor(repo Repository) *StockBalanceExecutor {
	return &StockBalanceExecutor{repo: repo}
}

func (r *StockBalanceExecutor) RoutePrefix() string { return "stock-balance" }
func (r *StockBalanceExecutor) Permission() string  { return "report:stock:read" }

func (r *StockBalanceExecutor) Meta() platform.ReportMeta {
	return platform.ReportMeta{
		Key:         "stock-balance",
		Name:        "Остатки товаров",
		Description: "Текущие остатки товаров на складах",
		Filters: []platform.ReportFilter{
			{Key: "asOfDate", Type: "date", Label: "На дату"},
			{Key: "warehouseId", Type: "reference", Label: "Склад", Ref: "warehouse", Multi: true},
			{Key: "productId", Type: "reference", Label: "Товар", Ref: "nomenclature", Multi: true},
			{Key: "excludeZero", Type: "boolean", Label: "Скрыть нулевые", Default: true},
		},
		Columns: []platform.ReportColumn{
			{Key: "warehouseId", Label: "Склад ID", Type: "reference", DefaultHidden: true},
			{Key: "warehouseName", Label: "Склад", Type: "string", Sortable: true},
			{Key: "productId", Label: "Товар ID", Type: "reference", DefaultHidden: true},
			{Key: "productName", Label: "Товар", Type: "string", Sortable: true},
			{Key: "productSku", Label: "Артикул", Type: "string", DefaultHidden: true},
			{Key: "unitName", Label: "Ед.", Type: "string"},
			{Key: "quantity", Label: "Остаток", Type: "quantity", Align: "right", Sortable: true},
		},
		GroupBy: []platform.ReportGroupBy{
			{Key: "warehouseName", Label: "По складу", DefaultActive: true},
		},
		Totals: []platform.ReportTotal{
			{Column: "quantity", Func: "sum", Label: "Итого"},
		},
		ExportFormats:   []string{"csv", "xlsx"},
		ScopeDimensions: []string{"warehouse"},
		DefaultSort:     &platform.ReportSort{Column: "productName", Direction: "asc"},
	}
}

// StockBalanceQueryFilter is the filter struct parsed from query params.
// Uses gin `form` tags for ShouldBindQuery.
type StockBalanceQueryFilter struct {
	AsOfDate     *time.Time `form:"asOfDate"`
	WarehouseIDs []string   `form:"warehouseId"`
	ProductIDs   []string   `form:"productId"`
	ExcludeZero  *bool      `form:"excludeZero"`
	Limit        int        `form:"limit"`
	Offset       int        `form:"offset"`
}

func (r *StockBalanceExecutor) Execute(ctx context.Context, filter StockBalanceQueryFilter) (*StockBalanceReport, error) {
	// Convert query filter to domain filter
	domainFilter := StockBalanceReportFilter{
		AsOfDate:    filter.AsOfDate,
		ExcludeZero: filter.ExcludeZero == nil || *filter.ExcludeZero,
		Limit:       filter.Limit,
		Offset:      filter.Offset,
	}

	for _, whStr := range filter.WarehouseIDs {
		if whID, err := id.Parse(whStr); err == nil {
			domainFilter.WarehouseIDs = append(domainFilter.WarehouseIDs, whID)
		}
	}
	for _, pStr := range filter.ProductIDs {
		if pID, err := id.Parse(pStr); err == nil {
			domainFilter.ProductIDs = append(domainFilter.ProductIDs, pID)
		}
	}

	// Default to current time if not specified
	if domainFilter.AsOfDate == nil {
		now := time.Now()
		domainFilter.AsOfDate = &now
	}

	// Set default pagination
	if domainFilter.Limit <= 0 {
		domainFilter.Limit = 100
	}
	if domainFilter.Limit > 1000 {
		domainFilter.Limit = 1000
	}

	report, err := r.repo.GetStockBalanceReport(ctx, domainFilter)
	if err != nil {
		return nil, fmt.Errorf("get stock balance report: %w", err)
	}

	return report, nil
}

// ---------------------------------------------------------------------------
// Stock Turnover Report (typed contract)
// ---------------------------------------------------------------------------

type StockTurnoverExecutor struct {
	repo Repository
}

func NewStockTurnoverExecutor(repo Repository) *StockTurnoverExecutor {
	return &StockTurnoverExecutor{repo: repo}
}

func (r *StockTurnoverExecutor) RoutePrefix() string { return "stock-turnover" }
func (r *StockTurnoverExecutor) Permission() string  { return "report:stock:read" }

func (r *StockTurnoverExecutor) Meta() platform.ReportMeta {
	return platform.ReportMeta{
		Key:         "stock-turnover",
		Name:        "Оборотная ведомость",
		Description: "Обороты товаров за период: начальный остаток, приход, расход, конечный остаток",
		Filters: []platform.ReportFilter{
			{Key: "fromDate", Type: "date", Label: "Начало периода", Required: true},
			{Key: "toDate", Type: "date", Label: "Конец периода", Required: true},
			{Key: "warehouseId", Type: "reference", Label: "Склад", Ref: "warehouse", Multi: true},
			{Key: "productId", Type: "reference", Label: "Товар", Ref: "nomenclature", Multi: true},
			{Key: "includeZero", Type: "boolean", Label: "Показать нулевые"},
		},
		Columns: []platform.ReportColumn{
			{Key: "warehouseId", Label: "Склад ID", Type: "reference", DefaultHidden: true},
			{Key: "warehouseName", Label: "Склад", Type: "string", Sortable: true},
			{Key: "productId", Label: "Товар ID", Type: "reference", DefaultHidden: true},
			{Key: "productName", Label: "Товар", Type: "string", Sortable: true},
			{Key: "productSku", Label: "Артикул", Type: "string", DefaultHidden: true},
			{Key: "unitName", Label: "Ед.", Type: "string"},
			{Key: "openingBalance", Label: "Нач. остаток", Type: "quantity", Align: "right", Sortable: true},
			{Key: "receipt", Label: "Приход", Type: "quantity", Align: "right", Sortable: true},
			{Key: "expense", Label: "Расход", Type: "quantity", Align: "right", Sortable: true},
			{Key: "closingBalance", Label: "Кон. остаток", Type: "quantity", Align: "right", Sortable: true},
		},
		GroupBy: []platform.ReportGroupBy{
			{Key: "warehouseName", Label: "По складу"},
			{Key: "productName", Label: "По товару", DefaultActive: true},
		},
		Totals: []platform.ReportTotal{
			{Column: "openingBalance", Func: "sum", Label: "Итого нач."},
			{Column: "receipt", Func: "sum", Label: "Итого приход"},
			{Column: "expense", Func: "sum", Label: "Итого расход"},
			{Column: "closingBalance", Func: "sum", Label: "Итого кон."},
		},
		ExportFormats:   []string{"csv", "xlsx"},
		ScopeDimensions: []string{"warehouse"},
		DefaultSort:     &platform.ReportSort{Column: "productName", Direction: "asc"},
	}
}

// StockTurnoverQueryFilter is the filter struct parsed from query params.
type StockTurnoverQueryFilter struct {
	FromDate     string   `form:"fromDate" binding:"required"`
	ToDate       string   `form:"toDate" binding:"required"`
	WarehouseIDs []string `form:"warehouseId"`
	ProductIDs   []string `form:"productId"`
	IncludeZero  bool     `form:"includeZero"`
	Limit        int      `form:"limit"`
	Offset       int      `form:"offset"`
}

func (r *StockTurnoverExecutor) Execute(ctx context.Context, filter StockTurnoverQueryFilter) (*StockTurnoverReport, error) {
	fromDate, err := time.Parse(time.RFC3339, filter.FromDate)
	if err != nil {
		return nil, fmt.Errorf("invalid fromDate format, expected RFC3339: %w", err)
	}
	toDate, err := time.Parse(time.RFC3339, filter.ToDate)
	if err != nil {
		return nil, fmt.Errorf("invalid toDate format, expected RFC3339: %w", err)
	}

	if fromDate.After(toDate) {
		return nil, fmt.Errorf("fromDate must be before toDate")
	}

	domainFilter := StockTurnoverReportFilter{
		FromDate:    fromDate,
		ToDate:      toDate,
		IncludeZero: filter.IncludeZero,
		Limit:       filter.Limit,
		Offset:      filter.Offset,
	}

	for _, whStr := range filter.WarehouseIDs {
		if whID, err := id.Parse(whStr); err == nil {
			domainFilter.WarehouseIDs = append(domainFilter.WarehouseIDs, whID)
		}
	}
	for _, pStr := range filter.ProductIDs {
		if pID, err := id.Parse(pStr); err == nil {
			domainFilter.ProductIDs = append(domainFilter.ProductIDs, pID)
		}
	}

	if domainFilter.Limit <= 0 {
		domainFilter.Limit = 100
	}
	if domainFilter.Limit > 1000 {
		domainFilter.Limit = 1000
	}

	report, err := r.repo.GetStockTurnoverReport(ctx, domainFilter)
	if err != nil {
		return nil, fmt.Errorf("get stock turnover report: %w", err)
	}

	return report, nil
}

// ---------------------------------------------------------------------------
// Document Journal Report (typed contract)
// ---------------------------------------------------------------------------

type DocumentJournalExecutor struct {
	repo Repository
}

func NewDocumentJournalExecutor(repo Repository) *DocumentJournalExecutor {
	return &DocumentJournalExecutor{repo: repo}
}

func (r *DocumentJournalExecutor) RoutePrefix() string { return "document-journal" }
func (r *DocumentJournalExecutor) Permission() string  { return "report:documents:read" }

func (r *DocumentJournalExecutor) Meta() platform.ReportMeta {
	return platform.ReportMeta{
		Key:         "document-journal",
		Name:        "Журнал документов",
		Description: "Список всех документов с фильтрацией и итогами",
		Filters: []platform.ReportFilter{
			{Key: "fromDate", Type: "date", Label: "С даты"},
			{Key: "toDate", Type: "date", Label: "По дату"},
			{Key: "documentType", Type: "enum", Label: "Тип документа", Multi: true},
			{Key: "posted", Type: "boolean", Label: "Проведён"},
			{Key: "number", Type: "string", Label: "Номер содержит"},
			{Key: "warehouseId", Type: "reference", Label: "Склад", Ref: "warehouse", Multi: true},
			{Key: "supplierId", Type: "reference", Label: "Поставщик", Ref: "counterparty", Multi: true},
		},
		Columns: []platform.ReportColumn{
			{Key: "documentType", Label: "Тип", Type: "string", Sortable: true},
			{Key: "number", Label: "Номер", Type: "string", Sortable: true},
			{Key: "date", Label: "Дата", Type: "date", Sortable: true},
			{Key: "posted", Label: "Проведён", Type: "boolean"},
			{Key: "counterpartyName", Label: "Контрагент", Type: "string"},
			{Key: "warehouseName", Label: "Склад", Type: "string"},
			{Key: "totalQuantity", Label: "Количество", Type: "quantity", Align: "right"},
			{Key: "totalAmount", Label: "Сумма", Type: "money", Align: "right"},
			{Key: "currency", Label: "Валюта", Type: "string"},
		},
		GroupBy: []platform.ReportGroupBy{
			{Key: "documentType", Label: "По типу документа"},
		},
		Totals: []platform.ReportTotal{
			{Column: "totalQuantity", Func: "sum", Label: "Итого кол-во"},
			{Column: "totalAmount", Func: "sum", Label: "Итого сумма"},
		},
		ExportFormats:   []string{"csv", "xlsx"},
		ScopeDimensions: []string{"warehouse"},
		DefaultSort:     &platform.ReportSort{Column: "date", Direction: "desc"},
	}
}

// DocumentJournalQueryFilter is the filter struct parsed from query params.
type DocumentJournalQueryFilter struct {
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

func (r *DocumentJournalExecutor) Execute(ctx context.Context, filter DocumentJournalQueryFilter) (*DocumentJournal, error) {
	domainFilter := DocumentJournalFilter{
		DocumentTypes:  filter.DocumentTypes,
		Posted:         filter.Posted,
		NumberContains: filter.NumberContains,
		SortBy:         filter.SortBy,
		SortOrder:      filter.SortOrder,
		Limit:          filter.Limit,
		Offset:         filter.Offset,
	}

	// Parse dates
	if filter.FromDate != nil {
		if t, err := time.Parse(time.RFC3339, *filter.FromDate); err == nil {
			domainFilter.FromDate = &t
		}
	}
	if filter.ToDate != nil {
		if t, err := time.Parse(time.RFC3339, *filter.ToDate); err == nil {
			domainFilter.ToDate = &t
		}
	}

	// Parse reference IDs
	for _, whStr := range filter.WarehouseIDs {
		if whID, err := id.Parse(whStr); err == nil {
			domainFilter.WarehouseIDs = append(domainFilter.WarehouseIDs, whID)
		}
	}
	for _, sStr := range filter.SupplierIDs {
		if sID, err := id.Parse(sStr); err == nil {
			domainFilter.SupplierIDs = append(domainFilter.SupplierIDs, sID)
		}
	}

	// Defaults
	if domainFilter.Limit <= 0 {
		domainFilter.Limit = 50
	}
	if domainFilter.Limit > 500 {
		domainFilter.Limit = 500
	}
	if domainFilter.SortBy == "" {
		domainFilter.SortBy = "date"
	}
	if domainFilter.SortOrder == "" {
		domainFilter.SortOrder = "desc"
	}

	journal, err := r.repo.GetDocumentJournal(ctx, domainFilter)
	if err != nil {
		return nil, fmt.Errorf("get document journal: %w", err)
	}

	// Get summary if on first page
	if domainFilter.Offset == 0 {
		summary, err := r.repo.GetDocumentTypeSummary(ctx, domainFilter)
		if err == nil {
			journal.Summary = summary
		}
	}

	return journal, nil
}
