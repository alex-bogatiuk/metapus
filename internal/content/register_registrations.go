package content

import (
	"github.com/gin-gonic/gin"

	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres/register_repo"

	"metapus/internal/domain/registers/stock"
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
	group.GET("/availability/:nomenclatureId", middleware.RequirePermission("register:stock:read"), stockHandler.GetNomenclatureAvailability)
}
