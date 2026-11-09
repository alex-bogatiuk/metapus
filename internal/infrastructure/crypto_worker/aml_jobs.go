package crypto_worker

import (
	"context"
	"time"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	amlruntime "metapus/internal/infrastructure/aml/runtime"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

// Jobs are opt-in through accepted mappings, explicit account enablement and
// finite package budget grants. Paid HTTP is never called inside a SQL tx.
func (p *CryptoProcessor) runAMLJobLoop(ctx context.Context) {
	repo := crypto_repo.NewAMLRepository()
	service := cryptoapp.NewAMLJobService(repo, amlruntime.NewResolver(repo, nil))
	preflight := cryptoapp.NewAMLPreflightService(repo, func(network id.ID) (crypto.EmptyCheckpointReader, error) {
		if p.drivers == nil {
			return nil, apperror.NewConflict("chain runtime unavailable")
		}
		driver, err := p.drivers.Resolve(network)
		if err != nil {
			return nil, err
		}
		reader, ok := driver.(crypto.EmptyCheckpointReader)
		if !ok {
			return nil, apperror.NewValidation("network has no independent checkpoint reader")
		}
		return reader, nil
	}, "aml-preflight-"+id.New().String())

	var verify func(context.Context) error
	if p.drivers != nil {
		verify = p.verifyFinalAMLInternalTransfers
	}
	cycle := cryptoapp.NewAMLMaintenanceCycle(repo, service, preflight, verify, p.log, time.Now)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cycle.Run(ctx)
		}
	}
}
