// Package integrationapp tests integration account connections.
package integrationapp

import (
	core "github.com/alex-bogatiuk/metapus/internal/application/integrationapp"
)

func NewConnectionTestService(targets core.ConnectionTargetRepository, prober core.ConnectionProber) *core.ConnectionTestService {
	return core.NewConnectionTestService(targets, prober)
}
