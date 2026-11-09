// Runtime facade keeps watcher references stable while replacing RPC clients.
package tron

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/blockchain/rpcpolicy"
)

type RPCPool struct {
	*rpcGeneration
	runtime *rpcpolicy.Runtime[rpcGeneration, RPCPoolConfig]
}

func NewRPCPool(cfg RPCPoolConfig) (*RPCPool, error) {
	gen, err := newRPCGeneration(cfg)
	if err != nil {
		return nil, err
	}
	r := rpcpolicy.NewRuntime(gen, cfg.InitialVersion, cfg.Reload, func(ctx context.Context, c RPCPoolConfig) (*rpcGeneration, error) {
		c.Reload = nil
		g, err := newRPCGeneration(c)
		if err == nil {
			g.Probe(ctx)
		}
		return g, err
	}, func(g *rpcGeneration) { g.Close() })
	return &RPCPool{rpcGeneration: gen, runtime: r}, nil
}
func (p *RPCPool) Close() {
	if p != nil && p.runtime != nil {
		p.runtime.Close()
	}
}
func (p *RPCPool) QuorumHead(ctx context.Context, role chainconfig.Role) (*BlockInfo, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *BlockInfo
		return zero0, err
	}
	defer release()
	return gen.QuorumHead(ctx, role)
}
func (p *RPCPool) ReadLatestBlock(ctx context.Context, height int64) (completeBlock, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 completeBlock
		return zero0, err
	}
	defer release()
	return gen.ReadLatestBlock(ctx, height)
}
func (p *RPCPool) ReadAgreedBlock(ctx context.Context, height int64) (completeBlock, []crypto.VerificationSource, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 completeBlock
		var zero1 []crypto.VerificationSource
		return zero0, zero1, err
	}
	defer release()
	return gen.ReadAgreedBlock(ctx, height)
}
func (p *RPCPool) LatestHead(ctx context.Context) (*BlockInfo, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *BlockInfo
		return zero0, err
	}
	defer release()
	return gen.LatestHead(ctx)
}
func (p *RPCPool) Status() []EndpointStatus {
	gen, revision, release, err := p.runtime.PeekSnapshot()
	if err != nil {
		return []EndpointStatus{{ProviderID: "configuration", State: BreakerInvalid, LastError: "configuration_unavailable"}}
	}
	defer release()
	statuses := gen.Status()
	for i := range statuses {
		statuses[i].AppliedRevision = revision
	}
	return statuses
}
func (p *RPCPool) Probe(ctx context.Context) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		return
	}
	defer release()
	gen.Probe(ctx)
}

func (p *RPCPool) CreateTransferTransaction(ctx context.Context, from, to string, amount int64) (*UnsignedTransaction, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *UnsignedTransaction
		return zero0, err
	}
	defer release()
	return gen.CreateTransferTransaction(ctx, from, to, amount)
}
func (p *RPCPool) CreateAccountTransaction(ctx context.Context, owner, account string) (*UnsignedTransaction, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *UnsignedTransaction
		return zero0, err
	}
	defer release()
	return gen.CreateAccountTransaction(ctx, owner, account)
}
func (p *RPCPool) TriggerSmartContract(ctx context.Context, params TriggerContractParams) (*UnsignedTransaction, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *UnsignedTransaction
		return zero0, err
	}
	defer release()
	return gen.TriggerSmartContract(ctx, params)
}
func (p *RPCPool) EstimateEnergy(ctx context.Context, params TriggerContractParams) (EnergyEstimate, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 EnergyEstimate
		return zero0, err
	}
	defer release()
	return gen.EstimateEnergy(ctx, params)
}
func (p *RPCPool) GetAccountResource(ctx context.Context, address string) (*AccountResourceResponse, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *AccountResourceResponse
		return zero0, err
	}
	defer release()
	return gen.GetAccountResource(ctx, address)
}
func (p *RPCPool) GetTRXBalance(ctx context.Context, address string) (types.CryptoAmount, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 types.CryptoAmount
		return zero0, err
	}
	defer release()
	return gen.GetTRXBalance(ctx, address)
}
func (p *RPCPool) GetTRC20Balance(ctx context.Context, address, contract string) (types.CryptoAmount, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 types.CryptoAmount
		return zero0, err
	}
	defer release()
	return gen.GetTRC20Balance(ctx, address, contract)
}
func (p *RPCPool) AccountExists(ctx context.Context, address string) (bool, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 bool
		return zero0, err
	}
	defer release()
	return gen.AccountExists(ctx, address)
}
func (p *RPCPool) GetTransactionInfo(ctx context.Context, txHash string) (*TransactionInfo, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *TransactionInfo
		return zero0, err
	}
	defer release()
	return gen.GetTransactionInfo(ctx, txHash)
}
func (p *RPCPool) GetCurrentBlock(ctx context.Context) (int64, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 int64
		return zero0, err
	}
	defer release()
	return gen.GetCurrentBlock(ctx)
}
func (p *RPCPool) GetCurrentBlockInfo(ctx context.Context) (*BlockInfo, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *BlockInfo
		return zero0, err
	}
	defer release()
	return gen.GetCurrentBlockInfo(ctx)
}
func (p *RPCPool) GetSolidifiedBlockByNumber(ctx context.Context, number int64) (*BlockInfo, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 *BlockInfo
		return zero0, err
	}
	defer release()
	return gen.GetSolidifiedBlockByNumber(ctx, number)
}

func (p *RPCPool) BroadcastSignedEnvelope(ctx context.Context, envelope crypto.SignedTxEnvelope) (crypto.BroadcastOutcome, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 crypto.BroadcastOutcome
		return zero0, err
	}
	defer release()
	return gen.BroadcastSignedEnvelope(ctx, envelope)
}
func (p *RPCPool) AgreedBlockHash(ctx context.Context, height int64) (string, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 string
		return zero0, err
	}
	defer release()
	return gen.AgreedBlockHash(ctx, height)
}
func (p *RPCPool) VerifyIncomingTransfer(ctx context.Context, network id.ID, hash, to, contract string) (crypto.TransferVerification, error) {
	gen, release, err := p.runtime.Acquire(ctx)
	if err != nil {
		var zero0 crypto.TransferVerification
		return zero0, err
	}
	defer release()
	return gen.VerifyIncomingTransfer(ctx, network, hash, to, contract)
}
