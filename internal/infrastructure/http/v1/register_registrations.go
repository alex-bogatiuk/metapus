// Package v1 — register_registrations.go contains concrete RouteRegistration
// implementations for accumulation registers and reports.
// These are the "business content" layer — specific registers/reports shipped with Metapus.
package v1

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/domain/registers/stock"
	"metapus/internal/domain/reports"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
	"metapus/internal/infrastructure/storage/postgres/report_repo"
)

// ---------------------------------------------------------------------------
// Accumulation Registers
// ---------------------------------------------------------------------------

// StockRegisterRegistration wires the Stock accumulation register endpoints.
type StockRegisterRegistration struct{}

func (r *StockRegisterRegistration) RoutePrefix() string { return "stock" }

func (r *StockRegisterRegistration) RegisterRoutes(group *gin.RouterGroup, cfg RouterConfig) {
	baseHandler := handlers.NewBaseHandler()
	stockRepo := register_repo.NewStockRepo()
	stockService := stock.NewService(stockRepo)
	stockHandler := handlers.NewStockHandler(baseHandler, stockService, stockRepo)

	group.GET("/balances", middleware.RequirePermission("register:stock:read"), stockHandler.GetBalances)
	group.GET("/movements", middleware.RequirePermission("register:stock:read"), stockHandler.GetMovements)
	group.GET("/turnovers", middleware.RequirePermission("register:stock:read"), stockHandler.GetTurnovers)
	group.GET("/availability/:productId", middleware.RequirePermission("register:stock:read"), stockHandler.GetProductAvailability)
}

// ---------------------------------------------------------------------------
// Reports
// ---------------------------------------------------------------------------

// StockBalanceReportRegistration wires the Stock Balance report endpoint.
type StockBalanceReportRegistration struct{}

func (r *StockBalanceReportRegistration) RoutePrefix() string { return "stock-balance" }

func (r *StockBalanceReportRegistration) RegisterRoutes(group *gin.RouterGroup, cfg RouterConfig) {
	baseHandler := handlers.NewBaseHandler()
	reportRepo := report_repo.NewReportRepo()
	reportService := reports.NewService(reportRepo)
	reportHandler := handlers.NewReportsHandler(baseHandler, reportService)

	group.GET("", middleware.RequirePermission("report:stock:read"), reportHandler.GetStockBalance)
}

// StockTurnoverReportRegistration wires the Stock Turnover report endpoint.
type StockTurnoverReportRegistration struct{}

func (r *StockTurnoverReportRegistration) RoutePrefix() string { return "stock-turnover" }

func (r *StockTurnoverReportRegistration) RegisterRoutes(group *gin.RouterGroup, cfg RouterConfig) {
	baseHandler := handlers.NewBaseHandler()
	reportRepo := report_repo.NewReportRepo()
	reportService := reports.NewService(reportRepo)
	reportHandler := handlers.NewReportsHandler(baseHandler, reportService)

	group.GET("", middleware.RequirePermission("report:stock:read"), reportHandler.GetStockTurnover)
}

// DocumentJournalReportRegistration wires the Document Journal report endpoint.
type DocumentJournalReportRegistration struct{}

func (r *DocumentJournalReportRegistration) RoutePrefix() string { return "document-journal" }

func (r *DocumentJournalReportRegistration) RegisterRoutes(group *gin.RouterGroup, cfg RouterConfig) {
	baseHandler := handlers.NewBaseHandler()
	reportRepo := report_repo.NewReportRepo()
	reportService := reports.NewService(reportRepo)
	reportHandler := handlers.NewReportsHandler(baseHandler, reportService)

	group.GET("", middleware.RequirePermission("report:documents:read"), reportHandler.GetDocumentJournal)
}
