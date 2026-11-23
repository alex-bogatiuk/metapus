package content

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/domain/registers/warehouse_stock"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/infrastructure/storage/postgres/stock_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/posting"
)

// Registers are the configuration's accumulation registers over PostgreSQL.
type Registers struct {
	WarehouseStock *warehouse_stock.Service
}

// NewRegisters creates the register services.
func NewRegisters() *Registers {
	return &Registers{WarehouseStock: warehouse_stock.NewService(stock_repo.New())}
}

// InstallPosting adds the registers' visitors and recorders to engine.
func (r *Registers) InstallPosting(engine *posting.Engine) []entity.MovementProvider {
	engine.AddVisitor(warehouse_stock.Visitor{})
	engine.AddRecorder(warehouse_stock.NewRecorder(r.WarehouseStock))
	return []entity.MovementProvider{r.WarehouseStock}
}

var _ posting.Module = (*Registers)(nil)
