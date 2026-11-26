// Package information declares information registers (регистры сведений)
// from their record type; the platform derives their table, writes, queries
// and the check of the table at tenant migration.
//
//	type Price struct {
//		information.RecordBase
//		ItemID     id.ID            `db:"item_id" meta:"label:Номенклатура,ref:item" register:"dimension"`
//		Price      types.MinorUnits `db:"price" meta:"label:Цена" register:"resource,currency=currency_id"`
//		CurrencyID id.ID            `db:"currency_id" meta:"label:Валюта,ref:currency" register:"resource"`
//	}
//
//	var Prices = information.New[Price](information.Definition{
//		Key: "item_prices", Label: "Цены номенклатуры", Table: "reg_item_prices",
//		Periodicity: information.Day, Mode: information.Recorder,
//	})
//
// The configuration registers it (registry.Registry.RegisterInformation) and
// creates its table in a migration (migration.InformationDDL prints it). An
// Independent register is written with Set and Delete; a Recorder register
// by documents that implement accumulation.Source and call Record.
package information

import (
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/domain/posting"
	core "github.com/alex-bogatiuk/metapus/internal/domain/registers/information"
)

type (
	// Register is an information register whose records have type R.
	Register[R any] = core.Register[R]
	// Definition names a register: key, label, table, periodicity, mode.
	Definition = core.Definition
	// Periodicity is NonPeriodic, Second or Day.
	Periodicity = core.Periodicity
	// Mode is Independent or Recorder.
	Mode = core.Mode
	// RecordBase is embedded by every record: period and recorder.
	RecordBase = core.RecordBase
	// Schema describes a register's table and fields.
	Schema = core.Schema
	// Field is a dimension or resource of a register.
	Field = core.Field
	// FieldType is how a field is stored and shown.
	FieldType = core.FieldType
	// Filter selects combinations of dimensions: column → value or values.
	Filter = core.Filter
	// Declared is a register whatever its record type.
	Declared = core.Declared
)

const (
	// NonPeriodic registers keep one record per combination of dimensions.
	NonPeriodic = core.NonPeriodic
	// Second registers keep a record per combination and second.
	Second = core.Second
	// Day registers keep a record per combination and day.
	Day = core.Day
	// Independent registers are written with Set and Delete.
	Independent = core.Independent
	// Recorder registers are written by posting documents.
	Recorder = core.Recorder
)

// New declares the register def with records of type R; it panics when the
// description is wrong.
func New[R any](def Definition) *Register[R] { return core.New[R](def) }

// Install adds the Recorder registers to a posting engine, for tests that
// post without the server (bind them first:
// Register.Bind(registers.NewInformationStore())).
func Install(engine *posting.Engine, registers ...Declared) []entity.MovementProvider {
	return core.Install(engine, registers...)
}
