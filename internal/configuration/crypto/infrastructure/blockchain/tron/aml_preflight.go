package tron

import (
	"context"
	"errors"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/pkg/sdk/types"
)

func (d *Driver) EmptyCheckpoint(ctx context.Context, asset crypto.AssetRef, address string) (crypto.EmptyCheckpoint, error) {
	var proof crypto.EmptyCheckpoint
	if err := d.Validate(address); err != nil {
		return proof, err
	}
	if asset.Standard != token.TokenStandardNative && asset.Standard != token.TokenStandardTRC20 {
		return proof, crypto.ErrVerificationMismatch
	}
	gen, release, err := d.rpc.runtime.Acquire(ctx)
	if err != nil {
		return proof, err
	}
	defer release()
	return withIndependentPair(ctx, gen, chainconfig.RoleSolidity, nil, func(ctx context.Context, pair []*rpcEndpoint) (crypto.EmptyCheckpoint, error) {
		readSource := func(ctx context.Context, endpoint *rpcEndpoint) (crypto.EmptyCheckpointSource, error) {
			before, err := gen.endpointHead(ctx, endpoint)
			if err != nil {
				return crypto.EmptyCheckpointSource{}, err
			}
			balance, err := pinnedRead(ctx, gen, endpoint, "AMLCheckpointBalance", func(ctx context.Context, client *Client) (types.RawAmount, error) {
				if asset.Standard == token.TokenStandardNative {
					return client.GetSolidifiedTRXBalance(ctx, address)
				}
				return client.GetSolidifiedTRC20Balance(ctx, address, asset.ContractAddress)
			})
			if err != nil {
				return crypto.EmptyCheckpointSource{}, err
			}
			after, err := gen.endpointHead(ctx, endpoint)
			if err != nil {
				return crypto.EmptyCheckpointSource{}, err
			}
			if before.BlockHeader.RawData.Number != after.BlockHeader.RawData.Number || !strings.EqualFold(before.BlockID, after.BlockID) {
				return crypto.EmptyCheckpointSource{}, crypto.ErrVerificationPending
			}
			return crypto.EmptyCheckpointSource{ProviderID: endpoint.providerID, TrustGroup: endpoint.trustGroup, Height: before.BlockHeader.RawData.Number, Hash: strings.ToLower(before.BlockID), Balance: balance, Method: "solidified_bracket"}, nil
		}
		// A stable observation remains valid after its node advances. Retain
		// each endpoint's observation and let a lagging peer reach that exact
		// height. Never compare balances from different checkpoints.
		values := make([]crypto.EmptyCheckpointSource, 2)
		for range 6 {
			for i, endpoint := range pair {
				if values[i].ProviderID != "" && values[i].Height >= values[1-i].Height {
					continue
				}
				value, err := readSource(ctx, endpoint)
				if err != nil {
					if errors.Is(err, crypto.ErrVerificationPending) {
						continue
					}
					return proof, &endpointFailure{endpoint: endpoint, cause: err}
				}
				values[i] = value
			}
			if values[0].ProviderID != "" && values[1].ProviderID != "" && values[0].Height == values[1].Height {
				if !strings.EqualFold(values[0].Hash, values[1].Hash) {
					return proof, crypto.ErrVerificationMismatch
				}
				result := crypto.EmptyCheckpoint{NetworkID: d.network.ID, Asset: asset, Address: address, Height: values[0].Height, Hash: values[0].Hash, ObservedAt: time.Now().UTC(), Sources: values}
				return result, result.Validate(time.Now())
			}
			timer := time.NewTimer(150 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return proof, ctx.Err()
			case <-timer.C:
			}
		}
		return proof, crypto.ErrVerificationPending
	})
}
