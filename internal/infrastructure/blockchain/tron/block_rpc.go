package tron

import (
	"context"
	"errors"
	"strings"
	"sync"

	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/providerpolicy"
)

func (p *rpcGeneration) independentPairExcept(role chainconfig.Role, excluded map[string]bool) ([]*rpcEndpoint, error) {
	var pair []*rpcEndpoint
	for _, endpoint := range p.readOrder(role, opBlock) {
		if endpointQuarantined(endpoint) {
			continue
		}
		endpoint.mu.Lock()
		headCircuit, blockCircuit := endpoint.circuit(opHead), endpoint.circuit(opBlock)
		unavailable := excluded[endpoint.trustGroup] || endpoint.unavailableUntil.After(p.now()) || headCircuit.OpenUntil.After(p.now()) || blockCircuit.OpenUntil.After(p.now()) || headCircuit.Unsupported || blockCircuit.Unsupported
		endpoint.mu.Unlock()
		if unavailable {
			continue
		}
		if endpoint.trustGroup == "" && p.settings.IngestionMode != "shadow" {
			continue
		}
		if len(pair) > 0 && ((endpoint.trustGroup != "" && endpoint.trustGroup == pair[0].trustGroup) || endpoint.providerID == pair[0].providerID || endpoint.client.baseURL == pair[0].client.baseURL) {
			continue
		}
		pair = append(pair, endpoint)
		if len(pair) == 2 {
			return pair, nil
		}
	}
	return nil, crypto.ErrChainQuorumUnavailable
}

func pinnedRead[T any](ctx context.Context, p *rpcGeneration, endpoint *rpcEndpoint, op string, call func(context.Context, *Client) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	// The enclosing quorum operation owns retries across independent pairs.
	// Repeating one pinned endpoint can consume its entire failover budget.
	if !p.beginReadContext(ctx, endpoint, op) {
		endpoint.mu.Lock()
		unsupported := endpoint.circuit(op).Unsupported
		oversize := endpoint.circuit(op).LastError == "response_limit"
		endpoint.mu.Unlock()
		if unsupported {
			return zero, providerpolicy.ErrUnsupported
		}
		if oversize {
			return zero, providerpolicy.ErrResponseTooLarge
		}
		return zero, crypto.ErrChainQuorumUnavailable
	}
	value, err := endpointRead(ctx, p, endpoint, op, call)
	if err != nil {
		return zero, &RPCReadFailure{Provider: endpoint.providerID, Operation: op, Category: rpcErrorCategory(err), cause: err}
	}
	return value, nil
}

func pairRead[T any](ctx context.Context, pair []*rpcEndpoint, call func(context.Context, *rpcEndpoint) (T, error)) ([]T, error) {
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	values := make([]T, len(pair))
	errorsByProvider := make([]error, len(pair))
	var wg sync.WaitGroup
	for i, endpoint := range pair {
		wg.Go(func() {
			values[i], errorsByProvider[i] = call(readCtx, endpoint)
			if errorsByProvider[i] != nil {
				if !errors.Is(errorsByProvider[i], context.Canceled) {
					errorsByProvider[i] = &endpointFailure{endpoint: endpoint, cause: errorsByProvider[i]}
				}
				cancel()
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errorsByProvider...); err != nil {
		return nil, err
	}
	return values, nil
}

func (p *rpcGeneration) endpointHead(ctx context.Context, endpoint *rpcEndpoint) (*BlockInfo, error) {
	return pinnedRead(ctx, p, endpoint, opHead, func(attempt context.Context, client *Client) (*BlockInfo, error) {
		var block *BlockInfo
		var err error
		if endpoint.role == chainconfig.RoleSolidity {
			block, err = client.GetSolidifiedCurrentBlockInfo(attempt)
		} else {
			block, err = client.GetCurrentBlockInfo(attempt)
		}
		if err == nil {
			err = p.observeHead(endpoint, block)
			if err == nil && endpointQuarantined(endpoint) {
				err = errRPCHeadLagging
			}
		}
		return block, err
	})
}

func (p *rpcGeneration) endpointBlock(ctx context.Context, endpoint *rpcEndpoint, height int64) (*BlockInfo, error) {
	return pinnedRead(ctx, p, endpoint, opBlock, func(attempt context.Context, client *Client) (*BlockInfo, error) {
		var block *BlockInfo
		var err error
		if endpoint.role == chainconfig.RoleSolidity {
			block, err = client.GetSolidifiedBlockByNumber(attempt, height)
		} else {
			block, err = client.GetBlockByNumber(attempt, height)
		}
		if err == nil {
			err = validateBlock(block, height)
		}
		return block, err
	})
}

// QuorumHead corroborates the same height/hash, rather than comparing two
// independently sampled tips that can legitimately differ by one block.
func (p *rpcGeneration) QuorumHead(ctx context.Context, role chainconfig.Role) (*BlockInfo, error) {
	return withIndependentPair(ctx, p, role, nil, p.quorumHeadPair)
}

func (p *rpcGeneration) quorumHeadPair(ctx context.Context, pair []*rpcEndpoint) (*BlockInfo, error) {
	heads, err := pairRead(ctx, pair, p.endpointHead)
	if err != nil {
		return nil, errors.Join(crypto.ErrChainQuorumUnavailable, err)
	}
	if err := p.checkPairLag(ctx, pair, heads); err != nil {
		return nil, err
	}
	height := min(heads[0].BlockHeader.RawData.Number, heads[1].BlockHeader.RawData.Number)
	for i, head := range heads {
		if head.BlockHeader.RawData.Number != height {
			heads[i], err = p.endpointBlock(ctx, pair[i], height)
			if err != nil {
				return nil, &endpointFailure{endpoint: pair[i], cause: err}
			}
		}
	}
	if !strings.EqualFold(heads[0].BlockID, heads[1].BlockID) {
		return nil, crypto.ErrVerificationMismatch
	}
	return heads[0], nil
}

func (p *rpcGeneration) completeEndpointBlock(ctx context.Context, endpoint *rpcEndpoint, height int64) (completeBlock, error) {
	block, err := p.endpointBlock(ctx, endpoint, height)
	if err != nil {
		return completeBlock{}, err
	}
	receipts, err := pinnedRead(ctx, p, endpoint, opBlockReceipts, func(attempt context.Context, client *Client) ([]TransactionInfo, error) {
		return client.GetBlockReceipts(attempt, height, endpoint.role == chainconfig.RoleSolidity)
	})
	if errors.Is(err, providerpolicy.ErrUnsupported) || errors.Is(err, providerpolicy.ErrResponseTooLarge) {
		receipts = make([]TransactionInfo, 0, len(block.Transactions))
		for _, tx := range block.Transactions {
			receipt, receiptErr := pinnedRead(ctx, p, endpoint, opReceipt, func(attempt context.Context, client *Client) (*TransactionInfo, error) {
				if endpoint.role == chainconfig.RoleSolidity {
					return client.GetSolidifiedTransactionInfo(attempt, tx.TxID)
				}
				return client.GetTransactionInfo(attempt, tx.TxID)
			})
			if receiptErr != nil {
				return completeBlock{}, receiptErr
			}
			if receipt == nil || receipt.ID == "" {
				return completeBlock{}, crypto.ErrVerificationPending
			}
			receipts = append(receipts, *receipt)
		}
		err = nil
	}
	if err != nil {
		return completeBlock{}, err
	}
	return assembleBlock(block, receipts)
}

func (p *rpcGeneration) ReadLatestBlock(ctx context.Context, height int64) (completeBlock, error) {
	var last error
	for _, endpoint := range p.readOrder(chainconfig.RoleFullNode, opBlock) {
		data, err := p.completeEndpointBlock(ctx, endpoint, height)
		if err == nil {
			return data, nil
		}
		last = err
		if semanticReadFailure(err) {
			return completeBlock{}, err
		}
		if ctx.Err() != nil {
			return completeBlock{}, ctx.Err()
		}
	}
	return completeBlock{}, errors.Join(crypto.ErrVerificationPending, last)
}

// A bounded scanner window pins both sources and shares agreed head reads.
// Every block and its full receipts are still independently fetched/compared.
type agreedBlockWindow struct {
	pair, latestPair   []*rpcEndpoint
	solidified, latest *BlockInfo
}

func (p *rpcGeneration) prepareBlockWindow(ctx context.Context) (agreedBlockWindow, error) {
	return p.prepareBlockWindowExcept(ctx, nil)
}

func (p *rpcGeneration) prepareBlockWindowExcept(ctx context.Context, excluded map[string]bool) (agreedBlockWindow, error) {
	return withIndependentPair(ctx, p, chainconfig.RoleSolidity, excluded, func(ctx context.Context, pair []*rpcEndpoint) (agreedBlockWindow, error) {
		solidified, err := p.quorumHeadPair(ctx, pair)
		if err != nil {
			return agreedBlockWindow{}, err
		}
		var latestPair []*rpcEndpoint
		for _, source := range pair {
			for _, endpoint := range p.readOrder(chainconfig.RoleFullNode, opHead) {
				if endpoint.trustGroup == source.trustGroup && (endpoint.providerID == source.providerID || p.settings.IngestionMode != "shadow") {
					if len(latestPair) > 0 && latestPair[0] == endpoint {
						continue
					}
					latestPair = append(latestPair, endpoint)
					break
				}
			}
		}
		// Shadow can compare unnamed endpoints, but never issues financial proofs.
		if p.settings.IngestionMode == "shadow" && len(latestPair) != 2 {
			latestPair, err = p.independentPairExcept(chainconfig.RoleFullNode, excluded)
		}
		if err != nil || len(latestPair) != 2 {
			return agreedBlockWindow{}, crypto.ErrChainQuorumUnavailable
		}
		latest, err := p.quorumHeadPair(ctx, latestPair)
		if err != nil {
			return agreedBlockWindow{}, err
		}
		return agreedBlockWindow{pair: pair, latestPair: latestPair, solidified: solidified, latest: latest}, nil
	})
}

func (p *rpcGeneration) ReadAgreedBlock(ctx context.Context, height int64) (completeBlock, []crypto.VerificationSource, error) {
	ctx, cancel := context.WithTimeout(ctx, p.settings.ReadBudget())
	defer cancel()
	window, err := p.prepareBlockWindow(ctx)
	if err != nil {
		return completeBlock{}, nil, err
	}
	return p.readAgreedWindowBlock(ctx, height, window)
}

func (p *rpcGeneration) readAgreedWindowBlock(ctx context.Context, height int64, window agreedBlockWindow) (completeBlock, []crypto.VerificationSource, error) {
	ctx, cancel := context.WithTimeout(ctx, p.settings.ReadBudget())
	defer cancel()
	excluded := make(map[string]bool)
	for range len(p.byRole[chainconfig.RoleSolidity]) + 1 {
		data, sources, err := p.readAgreedWindowBlockOnce(ctx, height, window)
		if err == nil || ctx.Err() != nil || !excludeFailedGroups(err, excluded) {
			return data, sources, err
		}
		window, err = p.prepareBlockWindowExcept(ctx, excluded)
		if err != nil {
			return completeBlock{}, nil, err
		}
	}
	return completeBlock{}, nil, crypto.ErrChainQuorumUnavailable
}

func (p *rpcGeneration) readAgreedWindowBlockOnce(ctx context.Context, height int64, window agreedBlockWindow) (completeBlock, []crypto.VerificationSource, error) {
	if window.solidified.BlockHeader.RawData.Number < height || window.latest.BlockHeader.RawData.Number < height {
		return completeBlock{}, nil, crypto.ErrVerificationPending
	}
	data, err := pairRead(ctx, window.pair, func(readCtx context.Context, endpoint *rpcEndpoint) (completeBlock, error) {
		return p.completeEndpointBlock(readCtx, endpoint, height)
	})
	if err != nil {
		return completeBlock{}, nil, errors.Join(crypto.ErrChainQuorumUnavailable, err)
	}
	if data[0].Digest != data[1].Digest {
		return completeBlock{}, nil, crypto.ErrVerificationMismatch
	}
	sources := make([]crypto.VerificationSource, 0, 2)
	for i, endpoint := range window.pair {
		sources = append(sources, crypto.VerificationSource{ProviderID: endpoint.providerID, LatestProviderID: window.latestPair[i].providerID, TrustGroup: endpoint.trustGroup, Digest: data[i].Digest, HeadHeight: window.latest.BlockHeader.RawData.Number, HeadHash: strings.ToLower(window.latest.BlockID), DecoderVersion: crypto.TRONTransferEvidenceSchema, SourceDigest: data[i].SourceDigest, ExcludedTraces: data[i].ExcludedTraces})
	}
	return data[0], sources, nil
}

func (p *rpcGeneration) LatestHead(ctx context.Context) (*BlockInfo, error) {
	return rpcRead(ctx, p, chainconfig.RoleFullNode, opHead, func(attempt context.Context, client *Client) (*BlockInfo, error) {
		block, err := client.GetCurrentBlockInfo(attempt)
		if err == nil && (block == nil || !validHash(block.BlockID) || block.BlockHeader.RawData.Number <= 0) {
			err = providerpolicy.ErrInvalidResponse
		}
		return block, err
	})
}
