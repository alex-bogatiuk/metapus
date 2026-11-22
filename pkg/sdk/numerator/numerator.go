// Package numerator generates document and catalog numbers.
package numerator

import (
	"metapus/internal/core/entity"
	core "metapus/internal/core/numerator"
)

type (
	// Config holds numbering configuration.
	Config = core.Config
	// Generator generates sequential document numbers.
	// This is the domain contract - implementations live in infrastructure layer.
	//
	// In Database-per-Tenant architecture, implementations should obtain
	// database connections from context using tenant.GetTxManager.
	Generator = core.Generator
	// MockGenerator is a test implementation of Generator.
	// Use in unit tests to avoid database dependencies.
	MockGenerator = core.MockGenerator
)

// DocumentSeries is the numerator configuration of a document's numbers:
// PREFIX-YEAR-00001 from a yearly series keyed by the prefix. Every service
// that numbers a document takes it from here, so the prefix lives only in the
// document's Definition.
func DocumentSeries(def entity.Definition) core.Config { return core.DocumentSeries(def) }

// Noop returns a Generator that panics on any call.
// Safe to pass to services that won't invoke numbering in the current context.
func Noop() core.Generator { return core.Noop() }
