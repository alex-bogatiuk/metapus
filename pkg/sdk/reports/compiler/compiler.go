// Package compiler compiles report queries to SQL.
package compiler

import (
	"metapus/internal/application/readaccess"
	"metapus/internal/domain/reports/schema"
	"metapus/internal/infrastructure/readpolicy"
	core "metapus/internal/infrastructure/reporting/compiler"
	"metapus/internal/metadata"
)

type (
	// Compiler is the core Query Engine.
	// Thread-safe after construction — all mutable state is per-request.
	Compiler = core.Compiler
	// QueryRequest is the JSON body sent by the frontend to execute a report.
	QueryRequest = core.QueryRequest
)

// NewCompiler creates a Compiler with the given metadata registry and datasets.
func NewCompiler(reg *metadata.Registry, datasets []*schema.Dataset, reads *readaccess.Service, bindings ...map[string]readpolicy.DatasetExecutor) *core.Compiler {
	return core.NewCompiler(reg, datasets, reads, bindings...)
}
