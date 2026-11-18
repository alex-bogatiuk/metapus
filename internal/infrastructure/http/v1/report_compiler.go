package v1

import (
	"metapus/internal/core/security"
	"metapus/internal/infrastructure/reporting/compiler"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/metadata"
)

// NewReportCompiler builds the report compiler of an assembled application
// from its factory registry. The HTTP report routes and the worker's
// scheduled reports both use it, so they see the same datasets and the same
// executors.
func NewReportCompiler(factories *FactoryRegistry, reg *metadata.Registry, policies *security.PolicyEngine) *compiler.Compiler {
	return compiler.NewCompiler(reg, factories.Datasets(), postgres.NewAuthorizedReadService(reg, policies), factories.DatasetExecutors())
}
