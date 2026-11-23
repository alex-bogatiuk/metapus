package v1

import (
	"github.com/alex-bogatiuk/metapus/internal/core/security"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/reporting/compiler"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

// NewReportCompiler builds the report compiler of an assembled application
// from its factory registry. The HTTP report routes and the worker's
// scheduled reports both use it, so they see the same datasets and the same
// executors.
func NewReportCompiler(factories *FactoryRegistry, reg *metadata.Registry, policies *security.PolicyEngine) *compiler.Compiler {
	return compiler.NewCompiler(reg, factories.Datasets(), postgres.NewAuthorizedReadService(reg, policies), factories.DatasetExecutors())
}
