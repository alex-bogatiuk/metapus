package evm

import (
	"context"
	"math/big"
	"strings"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/crypto"
)

func (d *Driver) EmptyCheckpoint(ctx context.Context, asset crypto.AssetRef, address string) (crypto.EmptyCheckpoint, error) {
	var proof crypto.EmptyCheckpoint
	normalized, err := NormalizeAddress(address)
	if err != nil {
		return proof, err
	}
	if asset.Standard != token.TokenStandardNative && !asset.Standard.IsEVMContract() {
		return proof, crypto.ErrVerificationMismatch
	}
	gen, release, err := d.rpc.runtime.Acquire(ctx)
	if err != nil {
		return proof, err
	}
	defer release()
	proof = crypto.EmptyCheckpoint{NetworkID: d.network.ID, Asset: asset, Address: normalized}
	for endpoint := range votingEndpoints(gen.readOrder(opState)) {
		if len(proof.Sources) > 0 && (endpoint.providerID == proof.Sources[0].ProviderID || endpoint.trustGroup == proof.Sources[0].TrustGroup) {
			continue
		}
		if !gen.beginRead(endpoint, opState, time.Now()) {
			continue
		}
		source, err := endpointRead(ctx, gen, endpoint, opState, func(ctx context.Context, client *ethclient.Client) (crypto.EmptyCheckpointSource, error) {
			height := finalizedBlockNumber()
			if len(proof.Sources) > 0 {
				height = big.NewInt(proof.Height)
			}
			before, err := client.HeaderByNumber(ctx, height)
			if err != nil {
				return crypto.EmptyCheckpointSource{}, err
			}
			if len(proof.Sources) > 0 {
				final, err := client.HeaderByNumber(ctx, finalizedBlockNumber())
				if err != nil || final.Number.Cmp(before.Number) < 0 {
					return crypto.EmptyCheckpointSource{}, crypto.ErrVerificationPending
				}
			}
			var balance *big.Int
			if asset.Standard == token.TokenStandardNative {
				balance, err = client.BalanceAt(ctx, common.HexToAddress(normalized), before.Number)
			} else {
				contract, e := NormalizeAddress(asset.ContractAddress)
				if e != nil {
					return crypto.EmptyCheckpointSource{}, e
				}
				data, e := erc20ABI.Pack("balanceOf", common.HexToAddress(normalized))
				if e != nil {
					return crypto.EmptyCheckpointSource{}, e
				}
				to := common.HexToAddress(contract)
				output, e := client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, before.Number)
				if e != nil {
					return crypto.EmptyCheckpointSource{}, e
				}
				values, e := erc20ABI.Unpack("balanceOf", output)
				if e != nil || len(values) != 1 {
					return crypto.EmptyCheckpointSource{}, crypto.ErrVerificationMismatch
				}
				balance, _ = values[0].(*big.Int)
			}
			if err != nil {
				return crypto.EmptyCheckpointSource{}, err
			}
			if balance == nil {
				return crypto.EmptyCheckpointSource{}, crypto.ErrVerificationMismatch
			}
			after, err := client.HeaderByNumber(ctx, before.Number)
			if err != nil {
				return crypto.EmptyCheckpointSource{}, err
			}
			if after.Hash() != before.Hash() {
				return crypto.EmptyCheckpointSource{}, crypto.ErrVerificationMismatch
			}
			amount, err := types.NewRawAmountFromBigInt(balance)
			if err != nil {
				return crypto.EmptyCheckpointSource{}, err
			}
			return crypto.EmptyCheckpointSource{ProviderID: endpoint.providerID, TrustGroup: endpoint.trustGroup, Height: before.Number.Int64(), Hash: strings.ToLower(before.Hash().Hex()), Balance: amount, Method: "finalized_block_bracket"}, nil
		})
		if err != nil {
			continue
		}
		if len(proof.Sources) == 0 {
			proof.Height, proof.Hash = source.Height, source.Hash
		}
		proof.Sources = append(proof.Sources, source)
		if len(proof.Sources) == 2 {
			proof.ObservedAt = time.Now().UTC()
			return proof, proof.Validate(time.Now())
		}
	}
	return crypto.EmptyCheckpoint{}, crypto.ErrChainQuorumUnavailable
}
