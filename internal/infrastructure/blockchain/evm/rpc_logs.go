package evm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"

	"metapus/internal/core/json"
)

// Match the explicit eth_getLogs range restriction, not arbitrary invalid
// requests, rate limits, or HTTP 400 responses. Never retain the remote text:
// upstream messages may contain URLs with credentials.
var logRangeRestriction = regexp.MustCompile(`(?i)eth_getLogs requests with up to (?:a )?([0-9]+) block range`)

func logRangeLimit(err error) uint64 {
	var message string
	var httpErr rpc.HTTPError
	var remote rpc.Error
	switch {
	case errors.As(err, &httpErr):
		if httpErr.StatusCode != http.StatusBadRequest {
			return 0
		}
		var envelope struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(httpErr.Body, &envelope) != nil || envelope.Error.Code != -32600 {
			return 0
		}
		message = envelope.Error.Message
	case errors.As(err, &remote) && remote.ErrorCode() == -32600:
		message = remote.Error()
	default:
		return 0
	}
	match := logRangeRestriction.FindStringSubmatch(message)
	if len(match) != 2 {
		return 0
	}
	limit, _ := strconv.ParseUint(match[1], 10, 64)
	return limit
}

// filterEndpointLogs keeps every subrequest inside the same admission slot,
// timeout, and read budget. Partial results are discarded on any failure. A
// learned limit only narrows later reads; recovery does not repeat the rejected
// large range or disable the endpoint's otherwise working logs operation.
func filterEndpointLogs(ctx context.Context, endpoint *rpcEndpoint, query ethereum.FilterQuery) ([]types.Log, error) {
	bounded := query.FromBlock != nil && query.ToBlock != nil && query.FromBlock.Sign() >= 0 && query.ToBlock.Cmp(query.FromBlock) >= 0
	if !bounded {
		logs, err := endpoint.client.FilterLogs(ctx, query)
		return logs, classifyLogsError(err)
	}
	var logs []types.Log
	first := true
	for from := new(big.Int).Set(query.FromBlock); from.Cmp(query.ToBlock) <= 0; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		endpoint.mu.Lock()
		limit := endpoint.logBlockRange
		endpoint.mu.Unlock()
		part := query
		part.FromBlock = new(big.Int).Set(from)
		part.ToBlock = new(big.Int).Set(query.ToBlock)
		if limit > 0 {
			last := new(big.Int).Add(from, new(big.Int).SetUint64(limit-1))
			if last.Cmp(part.ToBlock) < 0 {
				part.ToBlock = last
			}
		}
		if !first {
			if err := endpoint.admission.Wait(ctx); err != nil {
				return nil, errors.Join(ErrRPCAdmissionBusy, ctx.Err())
			}
		}
		first = false // endpointRead already admitted the first wire request.
		batch, err := endpoint.client.FilterLogs(ctx, part)
		if err != nil {
			if learned := logRangeLimit(err); learned > 0 {
				width := new(big.Int).Sub(part.ToBlock, part.FromBlock)
				width.Add(width, big.NewInt(1))
				if width.Cmp(new(big.Int).SetUint64(learned)) > 0 && (limit == 0 || learned < limit) {
					endpoint.mu.Lock()
					if endpoint.logBlockRange == 0 || learned < endpoint.logBlockRange {
						endpoint.logBlockRange = learned
					}
					endpoint.mu.Unlock()
					continue // Retry the same unconsumed height with the smaller range.
				}
			}
			return nil, classifyLogsError(err)
		}
		for _, log := range batch {
			height := new(big.Int).SetUint64(log.BlockNumber)
			if height.Cmp(part.FromBlock) < 0 || height.Cmp(part.ToBlock) > 0 {
				return nil, fmt.Errorf("EVM RPC returned log outside requested block range")
			}
		}
		logs = append(logs, batch...)
		from.Add(part.ToBlock, big.NewInt(1))
	}
	return logs, nil
}

func classifyLogsError(err error) error {
	var remote rpc.Error
	if errors.As(err, &remote) && remote.ErrorCode() == -32602 && strings.Contains(strings.ToLower(remote.Error()), "block range extends beyond current head block") {
		return ErrRPCHeadBehind
	}
	return err
}
