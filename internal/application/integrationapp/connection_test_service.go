package integrationapp

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
)

type ConnectionTargetRepository interface {
	ConnectionTarget(context.Context, id.ID) (integrations.ConnectionTarget, error)
}

type ConnectionProber interface {
	Probe(context.Context, integrations.ConnectionTarget) (bool, error)
}

type ConnectionTestService struct {
	targets ConnectionTargetRepository
	prober  ConnectionProber
}

func NewConnectionTestService(targets ConnectionTargetRepository, prober ConnectionProber) *ConnectionTestService {
	return &ConnectionTestService{targets: targets, prober: prober}
}

func (s *ConnectionTestService) Test(ctx context.Context, pool id.ID) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	target, err := s.targets.ConnectionTarget(ctx, pool)
	if err != nil {
		return false, err
	}
	return s.prober.Probe(ctx, target)
}
