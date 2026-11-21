package crypto_worker

import (
	"context"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
)

const _inboxRetryPeriod = 15 * time.Second

func (p *CryptoProcessor) runInboxRetryLoop(ctx context.Context) {
	ticker := time.NewTicker(_inboxRetryPeriod)
	defer ticker.Stop()
	p.retryInboundChainEvents(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.retryInboundChainEvents(ctx)
		}
	}
}

func (p *CryptoProcessor) retryInboundChainEvents(ctx context.Context) {
	if p.recorder != nil {
		p.recorder.RecordIfWork(ctx, "crypto.chain_event.inbox_retry", "crypto", func(ctx context.Context) (int, error) {
			return p.retryInboundChainEventsOnce(ctx)
		})
		return
	}
	if _, err := p.retryInboundChainEventsOnce(ctx); err != nil {
		p.log.Errorw("inbound chain event retry failed", "error", err)
	}
}

func (p *CryptoProcessor) retryInboundChainEventsOnce(ctx context.Context) (int, error) {
	inbox := crypto_repo.NewChainEventInboxRepo()
	poisoned := 0
	err := (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
		n, err := inbox.PoisonExhaustedRetries(txCtx)
		poisoned = n
		return err
	})
	if err != nil {
		return 0, err
	}

	var rows []crypto.InboundChainEvent
	err = (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
		claimed, err := inbox.ClaimReceivedForRetry(txCtx, 50, 30*time.Second)
		rows = claimed
		return err
	})
	if err != nil {
		return poisoned, err
	}

	retried := 0
	process := func(row crypto.InboundChainEvent) {
		event, err := crypto.RebuildBlockchainEvent(row)
		if err != nil {
			p.log.Errorw("rebuild inbound chain event failed", "id", row.ID, "error", err)
			return
		}
		if err := p.eventProcessor.ProcessEvent(ctx, event); err != nil {
			p.log.Errorw("retry inbound chain event failed",
				"id", row.ID,
				"tx_hash", event.TxHash,
				"error", err,
			)
			return
		}
		retried++
		recordInboxMetric(ctx, string(row.MatchStatus), row.MatchReason)
	}
	for _, row := range rows {
		process(row)
	}

	var unmatched []crypto.InboundChainEvent
	err = (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
		replay, err := inbox.ListUnmatchedForReplay(txCtx, crypto.MatchReasonNoAllocation, 50)
		unmatched = replay
		return err
	})
	if err != nil {
		return poisoned + retried, err
	}
	for _, row := range unmatched {
		process(row)
	}
	if poisoned > 0 {
		recordInboxMetric(ctx, string(crypto.InboxPoison), "retry_exhausted")
	}
	return poisoned + retried, nil
}
