// Package integration_repo is the runtime of integration accounts and
// pools.
package integration_repo

import (
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/integration_repo"
)

func NewRuntime(catalog *integrations.Catalog) *core.Runtime { return core.NewRuntime(catalog) }
