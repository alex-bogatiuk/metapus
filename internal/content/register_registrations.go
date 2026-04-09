package content

import (
	"github.com/gin-gonic/gin"

	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
	"metapus/internal/infrastructure/storage/postgres/report_repo"

	"metapus/internal/domain/registers/stock"
	"metapus/internal/domain/reports"
)

// ---------------------------------------------------------------------------
// Accumulation Registers
// ---------------------------------------------------------------------------

type StockRegisterRegistration struct{}

func (r *StockRegisterRegistration) RoutePrefix() string { return "stock" }

func (r *StockRegisterRegistration) RegisterRoutes(group *gin.RouterGroup, cfg v1.RouterConfig) {
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

type StockBalanceReportRegistration struct{}

func (r *StockBalanceReportRegistration) RoutePrefix() string { return "stock-balance" }

func (r *StockBalanceReportRegistration) RegisterRoutes(group *gin.RouterGroup, cfg v1.RouterConfig) {
	baseHandler := handlers.NewBaseHandler()
	reportRepo := report_repo.NewReportRepo()
	reportService := reports.NewService(reportRepo)
	reportHandler := handlers.NewReportsHandler(baseHandler, reportService)

	group.GET("", middleware.RequirePermission("report:stock:read"), reportHandler.GetStockBalance)
}

type StockTurnoverReportRegistration struct{}

func (r *StockTurnoverReportRegistration) RoutePrefix() string { return "stock-turnover" }

func (r *StockTurnoverReportRegistration) RegisterRoutes(group *gin.RouterGroup, cfg v1.RouterConfig) {
	baseHandler := handlers.NewBaseHandler()
	reportRepo := report_repo.NewReportRepo()
	reportService := reports.NewService(reportRepo)
	reportHandler := handlers.NewReportsHandler(baseHandler, reportService)

	group.GET("", middleware.RequirePermission("report:stock:read"), reportHandler.GetStockTurnover)
}

type DocumentJournalReportRegistration struct{}

func (r *DocumentJournalReportRegistration) RoutePrefix() string { return "document-journal" }

func (r *DocumentJournalReportRegistration) RegisterRoutes(group *gin.RouterGroup, cfg v1.RouterConfig) {
	baseHandler := handlers.NewBaseHandler()
	reportRepo := report_repo.NewReportRepo()
	reportService := reports.NewService(reportRepo)
	reportHandler := handlers.NewReportsHandler(baseHandler, reportService)

	group.GET("", middleware.RequirePermission("report:documents:read"), reportHandler.GetDocumentJournal)
}
