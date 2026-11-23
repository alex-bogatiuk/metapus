package crypto_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

const _inboundChainEventsTable = "reg_crypto_inbound_chain_events"

const _inboxColumns = `id, network_id, tx_hash, event_kind, event_index, from_address, to_address,
	token_contract, token_id, wallet_id, amount, block_number, block_hash, block_timestamp,
	confirmations, required_confs, finality_status, canonicality, event_type, direction, payload,
	match_status, match_reason, invoice_id, payment_id, allocation_id, receipt_id, details,
	retry_count, next_retry_at, version, created_at, updated_at`

type ChainEventInboxRepo struct{}

func NewChainEventInboxRepo() *ChainEventInboxRepo {
	return &ChainEventInboxRepo{}
}

var _ crypto.ChainEventInbox = (*ChainEventInboxRepo)(nil)

func (r *ChainEventInboxRepo) Upsert(ctx context.Context, ev crypto.InboundChainEvent) (crypto.InboundChainEvent, bool, error) {
	if ev.ID == (id.ID{}) {
		ev.ID = id.New()
	}
	if len(ev.Details) == 0 {
		ev.Details = json.RawMessage(`{}`)
	}
	if len(ev.Payload) == 0 {
		ev.Payload = json.RawMessage(`{}`)
	}
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var stored crypto.InboundChainEvent
	err := pgxscan.Get(ctx, querier, &stored, `
		INSERT INTO `+_inboundChainEventsTable+` (
		    id, network_id, tx_hash, event_kind, event_index,
		    from_address, to_address, token_contract, token_id, wallet_id,
		    amount, block_number, block_hash, block_timestamp,
		    confirmations, required_confs, finality_status, canonicality,
		    event_type, direction, payload, match_status, match_reason,
		    details, version
		) VALUES (
		    $1,$2,$3,$4,$5,
		    $6,$7,$8,$9,$10,
		    $11,$12,$13,$14,
		    $15,$16,$17,$18,
		    $19,$20,$21,$22,$23,
		    $24,1
		)
		ON CONFLICT (network_id, tx_hash, event_kind, event_index)
		    WHERE _deleted_at IS NULL
		DO UPDATE SET
		    confirmations = EXCLUDED.confirmations,
		    finality_status = EXCLUDED.finality_status,
		    canonicality = EXCLUDED.canonicality,
		    payload = EXCLUDED.payload,
		    version = `+_inboundChainEventsTable+`.version + 1
		RETURNING `+_inboxColumns+`
	`, ev.ID, ev.NetworkID, ev.TxHash, ev.EventKind, ev.EventIndex,
		ev.FromAddress, ev.ToAddress, ev.TokenContract, ev.TokenID, ev.WalletID,
		ev.Amount, ev.BlockNumber, ev.BlockHash, ev.BlockTimestamp,
		ev.Confirmations, ev.RequiredConfs, ev.FinalityStatus, ev.Canonicality,
		ev.EventType, ev.Direction, ev.Payload, crypto.InboxReceived, ev.MatchReason,
		ev.Details)
	if err != nil {
		return crypto.InboundChainEvent{}, false, fmt.Errorf("upsert inbound chain event: %w", err)
	}
	inserted := stored.ID == ev.ID && stored.MatchStatus == crypto.InboxReceived && stored.RetryCount == 0
	return stored, inserted, nil
}

func (r *ChainEventInboxRepo) MarkMatched(ctx context.Context, eventID, invoiceID, paymentID, allocationID id.ID) error {
	var allocArg any
	if allocationID != (id.ID{}) {
		allocArg = allocationID
	}
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	_, err := querier.Exec(ctx, `
		UPDATE `+_inboundChainEventsTable+`
		SET match_status = $2,
		    match_reason = '',
		    invoice_id = $3,
		    payment_id = $4,
		    allocation_id = $5,
		    next_retry_at = NULL,
		    version = version + 1
		WHERE id = $1
	`, eventID, crypto.InboxMatched, invoiceID, paymentID, allocArg)
	if err != nil {
		return fmt.Errorf("mark inbound chain event matched: %w", err)
	}
	return nil
}

func (r *ChainEventInboxRepo) MarkSinglePaymentReview(ctx context.Context, eventID, invoiceID, allocationID id.ID) error {
	var allocationArg any
	if allocationID != (id.ID{}) {
		allocationArg = allocationID
	}
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_inbound_chain_events
		SET match_status='needs_review',match_reason='additional_single_payment',
		    invoice_id=$2,allocation_id=$3,next_retry_at=NULL,version=version+1
		WHERE id=$1 AND match_status<>'resolved'
	`, eventID, invoiceID, allocationArg)
	if err != nil {
		return fmt.Errorf("mark additional payment for review: %w", err)
	}
	return nil
}

// UpdateReviewChainState keeps held transfers eligible only while canonical.
// Confirmation and reorg events do not create CryptoPayment rows for them.
func (r *ChainEventInboxRepo) UpdateReviewChainState(ctx context.Context, event crypto.BlockchainEvent) error {
	canonicality := string(event.Canonicality)
	if event.EventType == crypto.EventTypeReorg {
		canonicality = string(crypto.CanonicalityOrphaned)
	}
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_inbound_chain_events
		SET confirmations=$5,
		    finality_status=CASE WHEN $6='' THEN finality_status ELSE $6 END,
		    canonicality=CASE WHEN $7 IN ('','unknown') THEN canonicality ELSE $7 END,
		    version=version+1
		WHERE network_id=$1 AND tx_hash=$2 AND event_kind=$3 AND event_index=$4
		  AND match_status='needs_review' AND match_reason IN ('additional_single_payment','retired_deposit_address')
		  AND _deleted_at IS NULL
	`, event.NetworkID, event.TxHash, event.EffectiveEventKind(), event.EventIndex,
		event.Confirmations, string(event.Finality), canonicality)
	if err != nil {
		return fmt.Errorf("update held chain event: %w", err)
	}
	return nil
}

func (r *ChainEventInboxRepo) NextSinglePaymentReview(ctx context.Context, invoiceID id.ID) (*crypto.InboundChainEvent, error) {
	var event crypto.InboundChainEvent
	err := pgxscan.Get(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &event, `
		SELECT `+_inboxColumns+`
		FROM reg_crypto_inbound_chain_events
		WHERE invoice_id=$1 AND match_status='needs_review'
		  AND match_reason='additional_single_payment' AND canonicality='canonical'
		  AND receipt_id IS NULL
		  AND _deleted_at IS NULL
		ORDER BY created_at,id LIMIT 1 FOR UPDATE
	`, invoiceID)
	if pgxscan.NotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find next single-payment candidate: %w", err)
	}
	return &event, nil
}

func (r *ChainEventInboxRepo) MarkTerminal(
	ctx context.Context,
	eventID id.ID,
	status crypto.InboxMatchStatus,
	reason string,
	details map[string]any,
) error {
	payload := json.RawMessage(`{}`)
	if details != nil {
		encoded, err := json.Marshal(details)
		if err != nil {
			return fmt.Errorf("encode inbox details: %w", err)
		}
		payload = encoded
	}
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	_, err := querier.Exec(ctx, `
		UPDATE `+_inboundChainEventsTable+`
		SET match_status = $2,
		    match_reason = $3,
		    details = $4,
		    next_retry_at = CASE WHEN $2 = 'unmatched' THEN
		        NOW() + (LEAST(300, 5 * (1 << LEAST(retry_count, 6))) + FLOOR(RANDOM() * 3)) * INTERVAL '1 second'
		        ELSE NULL END,
		    version = version + 1
		WHERE id = $1
	`, eventID, status, reason, payload)
	if err != nil {
		return fmt.Errorf("mark inbound chain event terminal: %w", err)
	}
	return nil
}

func (r *ChainEventInboxRepo) ClaimReceivedForRetry(ctx context.Context, limit int, lease time.Duration) ([]crypto.InboundChainEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var rows []crypto.InboundChainEvent
	err := pgxscan.Select(ctx, querier, &rows, `
		UPDATE `+_inboundChainEventsTable+`
		SET retry_count = retry_count + 1,
		    next_retry_at = NOW() + $2::interval,
		    version = version + 1
		WHERE id IN (
		    SELECT id FROM `+_inboundChainEventsTable+`
		    WHERE match_status = $3
		      AND (next_retry_at IS NULL OR next_retry_at <= NOW())
		      AND retry_count < 8
		      AND _deleted_at IS NULL
		    ORDER BY created_at
		    FOR UPDATE SKIP LOCKED
		    LIMIT $1
		)
		RETURNING `+_inboxColumns+`
	`, limit, fmt.Sprintf("%d seconds", int(lease.Seconds())), crypto.InboxReceived)
	if err != nil {
		return nil, fmt.Errorf("claim inbound chain events for retry: %w", err)
	}
	return rows, nil
}

func (r *ChainEventInboxRepo) PoisonExhaustedRetries(ctx context.Context) (int, error) {
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	tag, err := querier.Exec(ctx, `
		UPDATE `+_inboundChainEventsTable+`
		SET match_status = $1,
		    match_reason = 'retry_exhausted',
		    next_retry_at = NULL,
		    version = version + 1
		WHERE match_status = $2
		  AND retry_count >= 8
		  AND _deleted_at IS NULL
	`, crypto.InboxPoison, crypto.InboxReceived)
	if err != nil {
		return 0, fmt.Errorf("poison exhausted inbound chain events: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *ChainEventInboxRepo) ListUnmatchedForReplay(ctx context.Context, reason string, limit int) ([]crypto.InboundChainEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	querier := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var rows []crypto.InboundChainEvent
	err := pgxscan.Select(ctx, querier, &rows, `
		UPDATE `+_inboundChainEventsTable+`
		SET retry_count = retry_count + 1,
		    next_retry_at = NOW() + INTERVAL '30 seconds',
		    version = version + 1
		WHERE id IN (
		    SELECT id FROM `+_inboundChainEventsTable+`
		    WHERE match_status = $1
		      AND match_reason = $2
		      AND receipt_id IS NULL
		      AND (next_retry_at IS NULL OR next_retry_at <= NOW())
		      AND _deleted_at IS NULL
		    ORDER BY next_retry_at NULLS FIRST, created_at, id
		    FOR UPDATE SKIP LOCKED
		    LIMIT $3
		)
		RETURNING `+_inboxColumns+`
	`, crypto.InboxUnmatched, reason, limit)
	if err != nil {
		return nil, fmt.Errorf("list unmatched inbound chain events: %w", err)
	}
	return rows, nil
}

// receiptEligibleReasons are unmatched outcomes of a supported asset on a
// managed wallet. Unknown tokens and wallets stay in the technical journal.
var receiptEligibleReasons = []string{
	crypto.MatchReasonNoAllocation, crypto.MatchReasonWrongToken, crypto.MatchReasonTimestampBeforeLease,
	crypto.MatchReasonOverlappingWindows, crypto.MatchReasonRetiredDepositAddress,
	crypto.MatchReasonAdditionalSinglePayment, crypto.MatchReasonDust,
}

// ClaimReceiptCandidates leases unmatched incoming transfers that still need a
// custody receipt. A transfer without an allocation stays eligible for late
// invoice matching during the grace period; an additional single-invoice
// payment may still be promoted until its invoice is terminal.
func (r *ChainEventInboxRepo) ClaimReceiptCandidates(ctx context.Context, limit int, lease, unallocatedGrace time.Duration) ([]crypto.InboundChainEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	// A transfer the Funding CLI already recognized has its financial owner:
	// retrying a receipt for it can never succeed and, once its RPC history is
	// pruned, retried forever.
	if _, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE `+_inboundChainEventsTable+` e
		SET match_status = $1, match_reason = $2, receipt_check_at = NULL, receipt_error = '', version = version + 1
		WHERE e.receipt_id IS NULL AND e._deleted_at IS NULL AND e.match_status IN ('unmatched','needs_review')
		  AND EXISTS (SELECT 1 FROM reg_crypto_wallet_fundings f
		      WHERE f.network_id = e.network_id AND f.wallet_id = e.wallet_id AND f.token_id = e.token_id
		        AND regexp_replace(LOWER(f.tx_hash), '^0x', '') = regexp_replace(LOWER(e.tx_hash), '^0x', '') AND f.amount = e.amount)
	`, crypto.InboxIgnored, crypto.MatchReasonWalletFunding); err != nil {
		return nil, fmt.Errorf("retire inbox events recognized as wallet funding: %w", err)
	}
	var rows []crypto.InboundChainEvent
	err := pgxscan.Select(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &rows, `
		UPDATE `+_inboundChainEventsTable+`
		SET receipt_check_at = NOW() + $2::interval, receipt_attempts = receipt_attempts + 1
		WHERE id IN (
		    SELECT id FROM `+_inboundChainEventsTable+`
		    WHERE receipt_id IS NULL AND _deleted_at IS NULL
		      AND match_status IN ('unmatched','needs_review') AND match_reason = ANY($3)
		      AND wallet_id IS NOT NULL AND token_id IS NOT NULL
		      AND direction IN ('incoming','') AND canonicality <> 'orphaned'
		      AND (receipt_check_at IS NULL OR receipt_check_at <= NOW())
		      AND (match_reason <> 'no_allocation' OR created_at <= NOW() - $4::interval)
		      AND (match_reason <> 'additional_single_payment' OR EXISTS (SELECT 1 FROM doc_crypto_invoices i
		          WHERE i.id = invoice_id AND i.status IN ('underpaid','completed','expired','cancelled','reversed')))
		    ORDER BY receipt_check_at NULLS FIRST, created_at, id
		    FOR UPDATE SKIP LOCKED
		    LIMIT $1
		)
		RETURNING `+_inboxColumns+`
	`, limit, fmt.Sprintf("%d seconds", int(lease.Seconds())), receiptEligibleReasons,
		fmt.Sprintf("%d seconds", int(unallocatedGrace.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("claim receipt candidates: %w", err)
	}
	return rows, nil
}

// LockReceiptCandidate re-reads the row inside the posting transaction. It
// returns false when the transfer was matched or accounted meanwhile.
func (r *ChainEventInboxRepo) LockReceiptCandidate(ctx context.Context, rowID id.ID) (*crypto.InboundChainEvent, bool, error) {
	var row crypto.InboundChainEvent
	err := pgxscan.Get(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &row, `
		SELECT `+_inboxColumns+` FROM `+_inboundChainEventsTable+`
		WHERE id = $1 AND receipt_id IS NULL AND _deleted_at IS NULL
		  AND match_status IN ('unmatched','needs_review') AND match_reason = ANY($2)
		FOR UPDATE`, rowID, receiptEligibleReasons)
	if pgxscan.NotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("lock receipt candidate: %w", err)
	}
	return &row, true, nil
}

func (r *ChainEventInboxRepo) MarkReceiptRecorded(ctx context.Context, rowID, receiptID id.ID) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE `+_inboundChainEventsTable+`
		SET receipt_id = $2, receipt_check_at = NULL, receipt_error = '', next_retry_at = NULL, version = version + 1
		WHERE id = $1 AND receipt_id IS NULL`, rowID, receiptID)
	if err != nil {
		return fmt.Errorf("link custody receipt to inbox: %w", err)
	}
	return nil
}

// DeferReceiptCandidate records why the receipt is still pending and backs off.
func (r *ChainEventInboxRepo) DeferReceiptCandidate(ctx context.Context, rowID id.ID, reason string) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE `+_inboundChainEventsTable+`
		SET receipt_error = left($2, 500),
		    receipt_check_at = NOW() + (LEAST(900, 15 * (1 << LEAST(receipt_attempts, 6)))) * INTERVAL '1 second'
		WHERE id = $1 AND receipt_id IS NULL`, rowID, reason)
	if err != nil {
		return fmt.Errorf("defer receipt candidate: %w", err)
	}
	return nil
}
