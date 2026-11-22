package evm

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"metapus/pkg/sdk/json"
)

// Check the wire hash as well as the decoded header. Otherwise a future fork
// field ignored by the client can silently produce incorrect persisted hashes.
func (p *rpcGeneration) readHeader(ctx context.Context, client *ethclient.Client, number *big.Int) (*types.Header, error) {
	tag := "latest"
	if number != nil {
		tag = hexutil.EncodeBig(number)
		if number.Sign() < 0 {
			tag = rpc.BlockNumber(number.Int64()).String()
		}
	}
	var raw json.RawMessage
	if err := client.Client().CallContext(ctx, &raw, "eth_getBlockByNumber", tag, false); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ethereum.NotFound
	}
	var header types.Header
	if err := header.UnmarshalJSON(raw); err != nil {
		return nil, ErrRPCInvalidHeader
	}
	var wire struct {
		Hash common.Hash `json:"hash"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil || wire.Hash == (common.Hash{}) || header.Hash() != wire.Hash || header.Number == nil || !header.Number.IsInt64() || header.Number.Sign() < 0 {
		return nil, ErrRPCInvalidHeader
	}
	if number != nil && number.Sign() >= 0 && header.Number.Cmp(number) != 0 {
		return nil, ErrRPCInvalidHeader
	}
	if number == nil {
		now := time.Now().UTC()
		stamp := time.Unix(int64(header.Time), 0)
		if stamp.Before(now.Add(-time.Duration(p.settings.MaxHeadAgeSeconds)*time.Second)) || stamp.After(now.Add(5*time.Second)) {
			return nil, ErrRPCStaleHead
		}
	}
	return &header, nil
}
