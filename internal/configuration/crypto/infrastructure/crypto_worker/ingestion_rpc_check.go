package crypto_worker

import (
	"context"
	"errors"

	"metapus/internal/configuration/crypto/domain/crypto"
)

func (p *CryptoProcessor) RunIngestionRPCCheck(ctx context.Context, networkCode string, options crypto.IngestionRPCCheckOptions) (crypto.IngestionRPCCheckResult, error) {
	networks, err := p.loadNetworksFiltered(ctx, false, networkCode)
	if err != nil {
		return crypto.IngestionRPCCheckResult{}, err
	}
	if networkCode == "" || len(networks) != 1 {
		return crypto.IngestionRPCCheckResult{}, errors.New("RPC check requires one explicit network code")
	}
	network := networks[0]
	drivers, err := p.cfg.DriverRegistryBuilder.Build(ctx, []crypto.NetworkRef{network.ref}, p.walletDerivers)
	defer closeIngestionDrivers(drivers)
	if err != nil {
		return crypto.IngestionRPCCheckResult{}, err
	}
	driver, err := drivers.Resolve(network.ref.ID)
	if err != nil {
		return crypto.IngestionRPCCheckResult{}, err
	}
	checker, ok := driver.(crypto.IngestionRPCChecker)
	if !ok {
		return crypto.IngestionRPCCheckResult{}, errors.New("network does not support the RPC capability check")
	}
	return checker.CheckIngestionRPC(ctx, network.assets, options)
}
