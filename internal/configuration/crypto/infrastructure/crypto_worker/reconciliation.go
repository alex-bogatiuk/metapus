package crypto_worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/token"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

const _reconciliationRequestPoll = 5 * time.Second
const _reconciliationRequestLease = 10 * time.Minute
const _reconciliationPageSize = 500

// ReconciliationResult describes one acquired reconciliation run.
type ReconciliationResult struct {
	RunID          id.ID
	Acquired       bool
	EligiblePairs  int
	AttemptedPairs int
	WalletsChecked int
	ItemsCreated   int
	ItemsUpdated   int
	ItemsResolved  int
	ReadErrors     int
}

func (p *CryptoProcessor) runReconciliationLoop(ctx context.Context) {
	ticker := time.NewTicker(_reconciliationPeriod)
	requestTicker := time.NewTicker(_reconciliationRequestPoll)
	defer ticker.Stop()
	defer requestTicker.Stop()

	p.log.Info("crypto reconciliation loop started")
	p.runScheduledReconciliation(ctx)
	p.runRequestedReconciliation(ctx)
	for {
		select {
		case <-ctx.Done():
			p.log.Info("crypto reconciliation loop stopped")
			return
		case <-ticker.C:
			p.runScheduledReconciliation(ctx)
		case <-requestTicker.C:
			p.runRequestedReconciliation(ctx)
		}
	}
}

func (p *CryptoProcessor) runScheduledReconciliation(ctx context.Context) {
	result, err := p.RunBalanceReconciliation(ctx)
	if err != nil {
		p.log.Errorw("crypto reconciliation failed", "error", err)
		return
	}
	if !result.Acquired {
		p.log.Debug("crypto reconciliation skipped because another worker owns the run lease")
	}
}

func (p *CryptoProcessor) runRequestedReconciliation(ctx context.Context) {
	requestID, walletID, claimed, err := p.claimReconciliationRequest(ctx)
	if err != nil {
		p.log.Errorw("claim requested crypto reconciliation", "error", err)
		return
	}
	if !claimed {
		return
	}
	result, runErr := p.RunWalletBalanceReconciliation(ctx, walletID)
	if err := p.finishRequestedReconciliation(ctx, requestID, result, runErr); err != nil {
		p.log.Errorw("finish requested crypto reconciliation", "request_id", requestID, "error", err)
	}
}

func (p *CryptoProcessor) claimReconciliationRequest(ctx context.Context) (id.ID, id.ID, bool, error) {
	return crypto_repo.NewWorkerRuntimeRepo().ClaimReconciliationRequest(ctx, _reconciliationRequestLease)
}

func (p *CryptoProcessor) finishRequestedReconciliation(ctx context.Context, requestID id.ID, result ReconciliationResult, runErr error) error {
	return crypto_repo.NewWorkerRuntimeRepo().FinishRequestedReconciliation(ctx, requestID, crypto_repo.WorkerReconciliationStats(result), runErr)
}

type reconciliationWalletBalance = crypto_repo.WorkerReconciliationWalletBalance

type reconciliationCursor = crypto_repo.WorkerReconciliationCursor

// RunBalanceReconciliation performs one leased reconciliation pass. It is also
// used by the dev preflight command before pool wallets become allocatable.
func (p *CryptoProcessor) RunBalanceReconciliation(ctx context.Context) (ReconciliationResult, error) {
	result, err := p.runBalanceReconciliation(ctx, nil, "")
	recordReconciliationRun(ctx, result, err)
	return result, err
}

// RunNetworkBalanceReconciliation scopes deployment preflight to the network
// whose driver was initialized. A single-network preflight must not report
// read failures for other active chain families that are intentionally absent
// from its temporary driver registry.
func (p *CryptoProcessor) RunNetworkBalanceReconciliation(
	ctx context.Context,
	networkCode string,
) (ReconciliationResult, error) {
	networkCode = strings.TrimSpace(networkCode)
	if networkCode == "" {
		return ReconciliationResult{}, errors.New("reconciliation network code is required")
	}
	result, err := p.runBalanceReconciliation(ctx, nil, networkCode)
	recordReconciliationRun(ctx, result, err)
	return result, err
}

// RunWalletBalanceReconciliation performs the same ledger-vs-chain checks for
// a newly provisioned wallet without waiting for the full scheduled scan.
func (p *CryptoProcessor) RunWalletBalanceReconciliation(
	ctx context.Context,
	walletID id.ID,
) (ReconciliationResult, error) {
	result, err := p.runBalanceReconciliation(ctx, &walletID, "")
	recordReconciliationRun(ctx, result, err)
	return result, err
}

func (p *CryptoProcessor) runBalanceReconciliation(
	ctx context.Context,
	walletID *id.ID,
	networkCode string,
) (ReconciliationResult, error) {
	result := ReconciliationResult{}
	runID, acquired, err := p.startReconciliationRun(ctx)
	if err != nil {
		return result, err
	}
	result.RunID = runID
	result.Acquired = acquired
	if !acquired {
		return result, nil
	}

	upper, err := p.reconciliationUpperBound(ctx, walletID, networkCode)
	if err != nil {
		_ = p.finishReconciliationRun(ctx, result, "failed", err.Error())
		return result, err
	}
	if upper != nil {
		result.EligiblePairs, err = p.reconciliationEligibleCount(ctx, walletID, networkCode, upper)
		if err != nil {
			_ = p.finishReconciliationRun(ctx, result, "failed", err.Error())
			return result, err
		}
	}

	type walletReadiness struct {
		allReadsSucceeded bool
		hasMismatch       bool
	}
	var currentWallet id.ID
	var currentState walletReadiness
	haveWallet := false
	service := p.balanceReconciler
	if service == nil {
		service = newBalanceReconciliationService()
	}
	completeWallet := func() error {
		if !haveWallet || !currentState.allReadsSucceeded || currentState.hasMismatch {
			return nil
		}
		if err := service.CompleteWalletCheck(ctx, currentWallet); err != nil {
			return fmt.Errorf("complete wallet reconciliation: %w", err)
		}
		return nil
	}
	var cursor *reconciliationCursor
	for upper != nil {
		wallets, err := p.loadReconciliationWalletsPage(ctx, walletID, networkCode, cursor, upper)
		if err != nil {
			_ = p.finishReconciliationRun(ctx, result, "failed", err.Error())
			return result, err
		}
		if len(wallets) == 0 {
			break
		}
		for _, walletBalance := range wallets {
			if !haveWallet || currentWallet != walletBalance.WalletID {
				if err := completeWallet(); err != nil {
					_ = p.finishReconciliationRun(ctx, result, "failed", err.Error())
					return result, err
				}
				currentWallet = walletBalance.WalletID
				currentState = walletReadiness{allReadsSucceeded: true}
				haveWallet = true
			}
			if err := p.heartbeatReconciliationRun(ctx, runID); err != nil {
				return result, err
			}
			result.AttemptedPairs++
			chainAmount, readErr := p.readChainBalance(ctx, walletBalance)
			if readErr != nil {
				currentState.allReadsSucceeded = false
				result.ReadErrors++
				p.log.Warnw("failed to read chain balance for reconciliation",
					"wallet_id", walletBalance.WalletID,
					"token_id", walletBalance.TokenID,
					"token_standard", walletBalance.TokenStandard,
					"error", readErr,
				)
				continue
			}
			if err := p.heartbeatReconciliationRun(ctx, runID); err != nil {
				return result, err
			}

			delta := chainAmount.Difference(walletBalance.LedgerAmount)
			if !delta.IsZero() {
				inFlight, err := crypto_repo.NewWorkerRuntimeRepo().InFlightInbound(ctx, walletBalance.WalletID, walletBalance.TokenID)
				if err != nil {
					_ = p.finishReconciliationRun(ctx, result, "failed", err.Error())
					return result, err
				}
				if inFlightExplainsDelta(chainAmount, walletBalance.LedgerAmount, inFlight) {
					// The chain is ahead by exactly the transfers awaiting their
					// payment or receipt: re-check next run instead of opening an
					// incident that takes the wallet out of allocation.
					currentState.allReadsSucceeded = false
					p.log.Infow("reconciliation deferred: chain delta equals in-flight inbound",
						"wallet_id", walletBalance.WalletID, "token_id", walletBalance.TokenID, "in_flight", inFlight.String())
					continue
				}
			}
			result.WalletsChecked++
			if !delta.IsZero() {
				currentState.hasMismatch = true
			}
			outcome, err := service.Record(ctx, crypto.BalanceObservation{
				RunID: runID, NetworkID: walletBalance.NetworkID, WalletID: walletBalance.WalletID,
				TokenID: walletBalance.TokenID, Kind: reconciliationKind(walletBalance.TokenStandard),
				LedgerAmount: walletBalance.LedgerAmount, ChainAmount: chainAmount,
			})
			if err != nil {
				_ = p.finishReconciliationRun(ctx, result, "failed", err.Error())
				return result, fmt.Errorf("persist balance reconciliation: %w", err)
			}
			if outcome.Created {
				result.ItemsCreated++
			}
			if outcome.Updated {
				result.ItemsUpdated++
			}
			if outcome.Resolved {
				result.ItemsResolved++
			}
		}
		last := wallets[len(wallets)-1]
		cursor = &reconciliationCursor{NetworkID: last.NetworkID, WalletID: last.WalletID, TokenID: last.TokenID}
		if len(wallets) < _reconciliationPageSize {
			break
		}
	}
	if err := completeWallet(); err != nil {
		_ = p.finishReconciliationRun(ctx, result, "failed", err.Error())
		return result, err
	}

	if err := p.finishReconciliationRun(ctx, result, "completed", ""); err != nil {
		return result, err
	}
	return result, nil
}

func newBalanceReconciliationService() *cryptoapp.BalanceReconciliationService {
	txm := contextTxManager{}
	return cryptoapp.NewBalanceReconciliationService(txm, crypto_repo.NewBalanceReconciliationRepo(),
		cryptoapp.NewOperationalHoldService(txm, crypto_repo.NewOperationalHoldRepo()))
}

func reconciliationKind(standard token.TokenStandard) string {
	if standard == token.TokenStandardNative {
		return crypto.BalanceMismatchNative
	}
	return crypto.BalanceMismatchToken
}

func (p *CryptoProcessor) readChainBalance(ctx context.Context, w reconciliationWalletBalance) (types.RawAmount, error) {
	driver, err := p.drivers.Resolve(w.NetworkID)
	if err != nil {
		return types.ZeroRawAmount(), err
	}
	if w.TokenStandard == token.TokenStandardNative {
		return driver.Balances().NativeBalance(ctx, w.Address)
	}
	return driver.Balances().TokenBalance(ctx, crypto.AssetRef{
		TokenID: w.TokenID, Standard: w.TokenStandard,
		ContractAddress: w.ContractAddress, Decimals: w.TokenDecimals,
	}, w.Address)
}

func (p *CryptoProcessor) reconciliationEligibleCount(ctx context.Context, walletID *id.ID, networkCode string, upper *reconciliationCursor) (int, error) {
	return crypto_repo.NewWorkerRuntimeRepo().ReconciliationEligibleCount(ctx, walletID, networkCode, upper)
}

func (p *CryptoProcessor) reconciliationUpperBound(ctx context.Context, walletID *id.ID, networkCode string) (*reconciliationCursor, error) {
	return crypto_repo.NewWorkerRuntimeRepo().ReconciliationUpperBound(ctx, walletID, networkCode)
}

// inFlightExplainsDelta is true only when the chain is ahead of the ledger by
// exactly the positive in-flight inbound amount. Any other delta, including a
// shortfall, remains a reconciliation incident.
func inFlightExplainsDelta(chain, ledger, inFlight types.RawAmount) bool {
	return inFlight.IsPositive() && chain.Cmp(ledger.Add(inFlight)) == 0
}

func (p *CryptoProcessor) loadReconciliationWalletsPage(ctx context.Context, walletID *id.ID, networkCode string, cursor, upper *reconciliationCursor) ([]reconciliationWalletBalance, error) {
	return crypto_repo.NewWorkerRuntimeRepo().LoadReconciliationWalletsPage(ctx, walletID, networkCode, cursor, upper, _reconciliationPageSize)
}

func (p *CryptoProcessor) startReconciliationRun(ctx context.Context) (id.ID, bool, error) {
	return crypto_repo.NewWorkerRuntimeRepo().StartReconciliationRun(ctx, p.workerID, _reconciliationLease)
}

func (p *CryptoProcessor) heartbeatReconciliationRun(ctx context.Context, runID id.ID) error {
	return crypto_repo.NewWorkerRuntimeRepo().HeartbeatReconciliationRun(ctx, runID, p.workerID, _reconciliationLease)
}

func (p *CryptoProcessor) finishReconciliationRun(ctx context.Context, result ReconciliationResult, status, errorMessage string) error {
	return crypto_repo.NewWorkerRuntimeRepo().FinishReconciliationRun(ctx, p.workerID, crypto_repo.WorkerReconciliationStats(result), status, errorMessage)
}
