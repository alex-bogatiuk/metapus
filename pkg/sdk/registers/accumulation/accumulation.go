// Package accumulation declares accumulation registers (регистры
// накопления) from their movement type, and the platform derives their
// storage, posting, movements view and queries.
//
// A register is a movement struct and a package variable:
//
//	type Movement struct {
//		entity.MovementBase
//		WarehouseID id.ID          `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse" register:"dimension"`
//		ItemID      id.ID          `db:"item_id" json:"itemId" meta:"label:Номенклатура,ref:item" register:"dimension"`
//		Quantity    types.Quantity `db:"quantity" json:"quantity" meta:"label:Количество" register:"resource"`
//	}
//
//	var Register = accumulation.New[Movement](accumulation.Definition{
//		Key: "warehouse_stock", Label: "Запасы на складах", Table: "reg_warehouse_stock", Kind: accumulation.Balances,
//	})
//
// The configuration registers it (registry.Registry.RegisterAccumulation)
// and creates its tables in a migration (migration.AccumulationDDL prints
// them). A document moves it by implementing Source. Definition.NonNegative
// refuses postings that take a balance below zero; a reserve is a dimension
// such as the customer order, empty (id.Nil) for the free balance.
package accumulation

import (
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
	core "github.com/alex-bogatiuk/metapus/internal/domain/registers/accumulation"
)

type (
	// Register is an accumulation register whose movements have type M.
	Register[M any] = core.Register[M]
	// Definition names a register: key, label, table prefix and kind.
	Definition = core.Definition
	// Kind is Balances or Turnovers.
	Kind = core.Kind
	// Schema describes a register's tables, dimensions and resources.
	Schema = core.Schema
	// Field is a dimension or resource of a register.
	Field = core.Field
	// FieldType is how a field is stored and shown.
	FieldType = core.FieldType
	// Filter selects combinations of dimensions: column → value or values.
	Filter = core.Filter
	// Source is a document that moves declarative registers.
	Source = core.Source
	// Movements collects a document's movements in its registers.
	Movements = core.Movements
	// Declared is a register whatever its movement type.
	Declared = core.Declared
	// Shortage is one resource a posting would take below zero in a
	// NonNegative register: the "shortages" detail of its error.
	Shortage = core.Shortage
	// DimensionValue is a dimension of a Shortage.
	DimensionValue = core.DimensionValue
)

const (
	// Balances registers keep a balance per combination of dimensions.
	Balances = core.Balances
	// Turnovers registers only sum their movements over a period.
	Turnovers = core.Turnovers
	// CodeInsufficientBalance is the code of the error that refuses a
	// posting which would take a NonNegative register below zero.
	CodeInsufficientBalance = core.CodeInsufficientBalance
)

// New declares the register def with movements of type M; it panics when
// the description is wrong.
func New[M any](def Definition) *Register[M] { return core.New[M](def) }

// Install adds registers to a posting engine, for tests that post without
// the server (bind them first: Register.Bind(registers.NewAccumulationStore())).
// The server installs and binds the registered ones itself.
func Install(engine *posting.Engine, registers ...Declared) []entity.MovementProvider {
	return core.Install(engine, registers...)
}
