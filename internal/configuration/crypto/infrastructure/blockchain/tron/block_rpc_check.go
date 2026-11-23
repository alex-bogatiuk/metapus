package tron

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
)

func (d *Driver) checkBlockIngestionRPC(ctx context.Context, options crypto.IngestionRPCCheckOptions) (crypto.IngestionRPCCheckResult, error) {
	result := crypto.IngestionRPCCheckResult{ObservedAt: time.Now().UTC(), NetworkCode: d.network.Code, Providers: []crypto.IngestionRPCProviderCheck{}}
	pool, release, acquireErr := d.rpc.runtime.Acquire(ctx)
	if acquireErr != nil {
		return result, acquireErr
	}
	defer release()
	result.DecoderVersion = crypto.TRONTransferEvidenceSchema
	comparisons := make(map[chainconfig.Role]map[int64][]crypto.IngestionRPCComparisonSource)

	if options.Samples < 1 || options.Samples > 10 || options.HistoricalBlock < 0 {
		return result, fmt.Errorf("TRON block RPC check requires 1..10 samples and nonnegative historical height")
	}
	head, err := pool.QuorumHead(ctx, chainconfig.RoleSolidity)
	if err != nil {
		return result, err
	}
	for i := range options.Samples {
		height := head.BlockHeader.RawData.Number - 2 - int64(i)
		if height <= 0 {
			return result, fmt.Errorf("insufficient TRON head history")
		}
		result.Heights = append(result.Heights, height)
		if options.HistoricalBlock > 0 {
			height = options.HistoricalBlock - int64(i)
			if height <= 0 {
				return result, fmt.Errorf("insufficient historical range")
			}
			if !slices.Contains(result.Heights, height) {
				result.Heights = append(result.Heights, height)
			}
		}
	}
	if options.InternalTransferTxHash != "" {
		if !validHash(options.InternalTransferTxHash) {
			return result, fmt.Errorf("internal transfer sample requires a complete transaction hash")
		}
		info, err := pool.GetTransactionInfo(ctx, options.InternalTransferTxHash)
		if err != nil {
			return result, err
		}
		if info == nil || info.BlockNumber <= 0 {
			return result, crypto.ErrVerificationPending
		}
		if !slices.Contains(result.Heights, info.BlockNumber) {
			result.Heights = append(result.Heights, info.BlockNumber)
		}
	}
	result.Ready = true
	for _, role := range []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity} {
		comparisons[role] = make(map[int64][]crypto.IngestionRPCComparisonSource)
		coverage := crypto.IngestionRPCRoleCoverage{Role: string(role), RequiredProviders: 2}
		groups := map[string]bool{}
		for _, endpoint := range pool.byRole[role] {
			positiveInternal := false
			provider := crypto.IngestionRPCProviderCheck{ProviderID: endpoint.providerID, Role: string(role), Ready: true}
			for _, operation := range []string{opHead, opBlock, opBlockReceipts} {
				check := crypto.IngestionRPCOperationCheck{Operation: operation, Errors: map[string]int{}}
				durations := make([]int64, 0, len(result.Heights))
				for _, height := range result.Heights {
					started := time.Now()
					var readErr error
					switch operation {
					case opHead:
						_, readErr = pool.endpointHead(ctx, endpoint)
					case opBlock:
						_, readErr = pool.endpointBlock(ctx, endpoint, height)
					case opBlockReceipts:
						var data completeBlock
						data, readErr = pool.completeEndpointBlock(ctx, endpoint, height)
						if readErr == nil {
							comparisons[role][height] = append(comparisons[role][height], crypto.IngestionRPCComparisonSource{ProviderID: endpoint.providerID, TrustGroup: endpoint.trustGroup, Digest: data.Digest, SourceDigest: data.SourceDigest, ExcludedTraces: data.ExcludedTraces})
							var events []crypto.BlockchainEvent
							events, readErr = decodeTransfers(d.network, data, crypto.FinalitySolidified, 1)
							for _, event := range events {
								if event.EventKind == crypto.EventKindInternalValue && event.Amount.IsPositive() && (options.InternalTransferTxHash == "" || event.TxHash == options.InternalTransferTxHash) {
									positiveInternal = true
								}
							}
						}
					}
					check.Attempted++
					if readErr == nil {
						check.Successful++
						durations = append(durations, time.Since(started).Milliseconds())
					} else {
						check.Errors[blockErrorCategory(readErr)]++
						if semanticReadFailure(readErr) {
							result.Ready = false
						}
						provider.Ready = false
					}
					if ctx.Err() != nil {
						return result, ctx.Err()
					}
				}
				slices.Sort(durations)
				if len(durations) > 0 {
					check.P50Millis = durations[(len(durations)-1)/2]
					check.P95Millis = durations[(len(durations)*95+99)/100-1]
				}
				provider.Operations = append(provider.Operations, check)
			}
			check := crypto.IngestionRPCOperationCheck{Operation: "positive_internal_transfer", Attempted: 1, Errors: map[string]int{}}
			if positiveInternal {
				check.Successful = 1
			} else {
				provider.Ready = false
				check.Errors["positive_sample_missing"] = 1
			}
			provider.Operations = append(provider.Operations, check)
			if provider.Ready && endpoint.trustGroup != "" && !groups[endpoint.trustGroup] {
				coverage.HealthyProviders++
				groups[endpoint.trustGroup] = true
			}
			result.Providers = append(result.Providers, provider)
		}
		if coverage.HealthyProviders < coverage.RequiredProviders {
			result.Ready = false
		}
		result.RoleCoverage = append(result.RoleCoverage, coverage)
		for _, height := range result.Heights {
			sources := comparisons[role][height]
			comparison := crypto.IngestionRPCComparison{Role: string(role), Height: height, Sources: sources, Matched: true}
			groups := map[string]bool{}
			for i, source := range sources {
				if source.TrustGroup != "" {
					groups[source.TrustGroup] = true
				}
				if i > 0 && source.Digest != sources[0].Digest {
					comparison.Matched = false
				}
			}
			if len(groups) < 2 {
				comparison.Matched = false
			}
			if !comparison.Matched {
				result.Ready = false
			}
			result.Comparisons = append(result.Comparisons, comparison)
		}
	}
	return result, nil
}
