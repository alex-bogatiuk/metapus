// Package integrationapp tests integration account connections.
package integrationapp

import (
	core "metapus/internal/application/integrationapp"
)

func NewConnectionTestService(targets core.ConnectionTargetRepository, prober core.ConnectionProber) *core.ConnectionTestService {
	return core.NewConnectionTestService(targets, prober)
}
