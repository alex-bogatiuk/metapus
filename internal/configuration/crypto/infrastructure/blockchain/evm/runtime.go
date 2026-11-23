// Runtime facade keeps watcher references stable while replacing RPC clients.
package evm

import (
	"context"
	"math/big"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/rpcpolicy"
)

type RPCPool struct {
	*rpcGeneration
	runtime *rpcpolicy.Runtime[rpcGeneration, RPCPoolConfig]
}

// acquireReadWindow pins both clients and limits for a bounded watcher poll.
// A larger new response cap cannot enter an old prefetch/memory window.
func (p *RPCPool) acquireReadWindow(ctx context.Context) (watcherRPC, chainconfig.EVMSettings, func(), error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		return nil, chainconfig.EVMSettings{}, release, err
	}
	return gen, gen.settings, release, nil
}

func NewRPCPool(ctx context.Context, cfg RPCPoolConfig) (*RPCPool, error) {
	gen, err := newRPCGeneration(ctx, cfg)
	if err != nil {
		return nil, err
	}
	r := rpcpolicy.NewRuntime(gen, cfg.InitialVersion, cfg.Reload, func(ctx context.Context, c RPCPoolConfig) (*rpcGeneration, error) {
		c.Reload = nil
		c.allowWrongChainEndpoints = true
		return newRPCGeneration(ctx, c)
	}, func(g *rpcGeneration) { g.Close() })
	return &RPCPool{rpcGeneration: gen, runtime: r}, nil
}
func (p *RPCPool) Close() {
	if p != nil && p.runtime != nil {
		p.runtime.Close()
	}
}
func (p *RPCPool) Probe(ctx context.Context) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		return
	}
	defer release()
	gen.Probe(ctx)
}
func (p *RPCPool) Status() []crypto.RuntimeEndpointStatus {
	gen, revision, release, err := p.runtime.PeekSnapshot()
	if err != nil {
		return []crypto.RuntimeEndpointStatus{{ProviderID: "configuration", Role: "rpc", State: "invalid", LastError: "configuration_unavailable"}}
	}
	defer release()
	statuses := gen.Status()
	for i := range statuses {
		statuses[i].AppliedRevision = revision
	}
	return statuses
}
func (p *RPCPool) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *types.Header
		return zero0, err
	}
	defer release()
	return gen.HeaderByNumber(ctx, number)
}
func (p *RPCPool) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *types.Block
		return zero0, err
	}
	defer release()
	return gen.BlockByNumber(ctx, number)
}
func (p *RPCPool) TransactionReceipt(ctx context.Context, hash [32]byte) (*types.Receipt, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *types.Receipt
		return zero0, err
	}
	defer release()
	return gen.TransactionReceipt(ctx, hash)
}
func (p *RPCPool) TransactionByHash(ctx context.Context, hash [32]byte) (*types.Transaction, bool, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *types.Transaction
		var zero1 bool
		return zero0, zero1, err
	}
	defer release()
	return gen.TransactionByHash(ctx, hash)
}
func (p *RPCPool) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 []types.Log
		return zero0, err
	}
	defer release()
	return gen.FilterLogs(ctx, query)
}
func (p *RPCPool) BalanceAt(ctx context.Context, address [20]byte) (*big.Int, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *big.Int
		return zero0, err
	}
	defer release()
	return gen.BalanceAt(ctx, address)
}
func (p *RPCPool) PendingNonceAt(ctx context.Context, address [20]byte) (uint64, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 uint64
		return zero0, err
	}
	defer release()
	return gen.PendingNonceAt(ctx, address)
}
func (p *RPCPool) NonceAt(ctx context.Context, address [20]byte) (uint64, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 uint64
		return zero0, err
	}
	defer release()
	return gen.NonceAt(ctx, address)
}
func (p *RPCPool) ObserveNonceState(ctx context.Context, sourceAddress string) (ProviderNonceSnapshot, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 ProviderNonceSnapshot
		return zero0, err
	}
	defer release()
	return gen.ObserveNonceState(ctx, sourceAddress)
}
func (p *RPCPool) CanonicalHeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *types.Header
		return zero0, err
	}
	defer release()
	return gen.CanonicalHeaderByNumber(ctx, number)
}
func (p *RPCPool) EstimateGas(ctx context.Context, message ethereum.CallMsg) (uint64, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 uint64
		return zero0, err
	}
	defer release()
	return gen.EstimateGas(ctx, message)
}
func (p *RPCPool) FeeHistory(ctx context.Context, blockCount uint64, rewardPercentiles []float64) (*ethereum.FeeHistory, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *ethereum.FeeHistory
		return zero0, err
	}
	defer release()
	return gen.FeeHistory(ctx, blockCount, rewardPercentiles)
}
func (p *RPCPool) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *big.Int
		return zero0, err
	}
	defer release()
	return gen.SuggestGasTipCap(ctx)
}
func (p *RPCPool) CallContract(ctx context.Context, message ethereum.CallMsg) ([]byte, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 []byte
		return zero0, err
	}
	defer release()
	return gen.CallContract(ctx, message)
}
func (p *RPCPool) CodeAt(ctx context.Context, address [20]byte) ([]byte, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 []byte
		return zero0, err
	}
	defer release()
	return gen.CodeAt(ctx, address)
}
func (p *RPCPool) SendTransaction(ctx context.Context, tx *types.Transaction) (string, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 string
		return zero0, err
	}
	defer release()
	return gen.SendTransaction(ctx, tx)
}
func (p *RPCPool) assessReplacement(ctx context.Context, source [20]byte, nonce uint64, hashes [][32]byte) crypto.ReplacementAssessment {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		return crypto.ReplacementAssessment{Disposition: crypto.ReplacementUncertain, Reason: "RPC configuration unavailable"}
	}
	defer release()
	return gen.assessReplacement(ctx, source, nonce, hashes)
}
