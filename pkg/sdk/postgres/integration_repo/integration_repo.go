// Package integration_repo is the runtime of integration accounts and
// pools.
package integration_repo

import (
	"metapus/internal/domain/integrations"
	core "metapus/internal/infrastructure/storage/postgres/integration_repo"
)

func NewRuntime(catalog *integrations.Catalog) *core.Runtime { return core.NewRuntime(catalog) }
