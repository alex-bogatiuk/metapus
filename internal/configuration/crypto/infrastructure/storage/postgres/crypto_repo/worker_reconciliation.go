package crypto_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/storage/postgres"
)

type WorkerReconciliationWalletBalance struct {
	WalletID        id.ID
	NetworkID       id.ID
	Address         string
	TokenID         id.ID
	ContractAddress string
	TokenStandard   token.TokenStandard
	TokenDecimals   int
	LedgerAmount    types.RawAmount
}

type WorkerReconciliationCursor struct {
	NetworkID  id.ID
	WalletID   id.ID
	TokenID    id.ID
	SnapshotAt time.Time
}

// WorkerReconciliationStats describes one acquired reconciliation run.
type WorkerReconciliationStats struct {
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

const _reconciliationPairsFrom = `
		FROM cat_wallets w
		JOIN cat_blockchain_networks n
		  ON n.id = w.network_id
		 AND n.is_active = TRUE
		 AND n.deletion_mark = FALSE
		JOIN cat_tokens t
		  ON t.network_id = w.network_id
		 AND t.is_active = TRUE
		 AND t.deletion_mark = FALSE
		 AND (t.token_standard = $1 OR COALESCE(t.contract_address, '') <> '')
		LEFT JOIN reg_crypto_balance_balances b
		  ON b.wallet_id = w.id
		 AND b.token_id = t.id
		WHERE w.is_active = TRUE
		  AND w.deletion_mark = FALSE
		  AND w.address <> ''
		  AND ($2::uuid IS NULL OR w.id = $2)
		  AND ($3 = '' OR n.code = $3)
		  AND w.created_at <= $4
		  AND t.created_at <= $4
	`

func (r *WorkerRuntimeRepo) ClaimReconciliationRequest(ctx context.Context, lease time.Duration) (id.ID, id.ID, bool, error) {
	txm := postgres.MustGetTxManager(ctx)
	var requestID, walletID id.ID
	claimed := false
	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		if _, err := q.Exec(txCtx, `
			UPDATE reg_crypto_reconciliation_requests
			SET status='pending',claimed_at=NULL,last_error='request lease expired'
			WHERE status='running' AND claimed_at<NOW()-($1 * INTERVAL '1 second')
		`, int64(lease.Seconds())); err != nil {
			return err
		}
		err := q.QueryRow(txCtx, `
			SELECT id,wallet_id FROM reg_crypto_reconciliation_requests
			WHERE status='pending' ORDER BY requested_at,id
			FOR UPDATE SKIP LOCKED LIMIT 1
		`).Scan(&requestID, &walletID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		tag, err := q.Exec(txCtx, `
			UPDATE reg_crypto_reconciliation_requests
			SET status='running',claimed_at=NOW(),attempts=attempts+1,last_error=''
			WHERE id=$1 AND status='pending'
		`, requestID)
		if err != nil {
			return err
		}
		claimed = tag.RowsAffected() == 1
		return nil
	})
	return requestID, walletID, claimed, err
}

func (r *WorkerRuntimeRepo) FinishRequestedReconciliation(ctx context.Context, requestID id.ID, result WorkerReconciliationStats, runErr error) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if runErr == nil && result.Acquired && result.ReadErrors == 0 {
		_, err := q.Exec(ctx, `
			UPDATE reg_crypto_reconciliation_requests
			SET status='completed',completed_at=NOW(),last_error=''
			WHERE id=$1 AND status='running'
		`, requestID)
		return err
	}
	message := "reconciliation lease was not acquired"
	if runErr != nil {
		message = runErr.Error()
	} else if result.ReadErrors > 0 {
		message = fmt.Sprintf("reconciliation completed with %d chain read errors", result.ReadErrors)
	}
	_, err := q.Exec(ctx, `
		UPDATE reg_crypto_reconciliation_requests
		SET status=CASE WHEN attempts>=10 THEN 'failed' ELSE 'pending' END,
		    claimed_at=CASE WHEN attempts>=10 THEN claimed_at ELSE NULL END,
		    completed_at=CASE WHEN attempts>=10 THEN NOW() ELSE NULL END,
		    last_error=$2
		WHERE id=$1 AND status='running'
	`, requestID, message)
	return err
}

func (r *WorkerRuntimeRepo) ReconciliationEligibleCount(
	ctx context.Context, walletID *id.ID, networkCode string, upper *WorkerReconciliationCursor,
) (int, error) {
	query := `SELECT COUNT(*) ` + _reconciliationPairsFrom +
		` AND (w.network_id,w.id,t.id) <= ($5::uuid,$6::uuid,$7::uuid)`
	var count int
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, query,
		token.TokenStandardNative, walletID, networkCode, upper.SnapshotAt,
		upper.NetworkID, upper.WalletID, upper.TokenID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count eligible reconciliation pairs: %w", err)
	}
	return count, nil
}

// The upper tuple bounds one run. New pairs beyond it are covered next run.
func (r *WorkerRuntimeRepo) ReconciliationUpperBound(
	ctx context.Context, walletID *id.ID, networkCode string,
) (*WorkerReconciliationCursor, error) {
	var upper WorkerReconciliationCursor
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx,
		`SELECT statement_timestamp()`).Scan(&upper.SnapshotAt)
	if err != nil {
		return nil, fmt.Errorf("start reconciliation snapshot: %w", err)
	}
	query := `SELECT w.network_id,w.id,t.id ` + _reconciliationPairsFrom +
		` ORDER BY w.network_id DESC,w.id DESC,t.id DESC LIMIT 1`
	err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).
		QueryRow(ctx, query, token.TokenStandardNative, walletID, networkCode, upper.SnapshotAt).
		Scan(&upper.NetworkID, &upper.WalletID, &upper.TokenID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find reconciliation upper bound: %w", err)
	}
	return &upper, nil
}

func (r *WorkerRuntimeRepo) LoadReconciliationWalletsPage(
	ctx context.Context, walletID *id.ID, networkCode string,
	cursor, upper *WorkerReconciliationCursor, limit int,
) ([]WorkerReconciliationWalletBalance, error) {
	var cursorNetwork, cursorWallet, cursorToken any
	if cursor != nil {
		cursorNetwork, cursorWallet, cursorToken = cursor.NetworkID, cursor.WalletID, cursor.TokenID
	}
	query := `
		SELECT w.id,w.network_id,w.address,t.id,
		       COALESCE(t.contract_address, ''),t.token_standard,t.decimal_places,
		       COALESCE(b.amount, 0)
	` + _reconciliationPairsFrom + `
		  AND ($5::uuid IS NULL OR (w.network_id,w.id,t.id) > ($5::uuid,$6::uuid,$7::uuid))
		  AND (w.network_id,w.id,t.id) <= ($8::uuid,$9::uuid,$10::uuid)
		ORDER BY w.network_id,w.id,t.id
		LIMIT $11
	`
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, query,
		token.TokenStandardNative, walletID, networkCode, upper.SnapshotAt,
		cursorNetwork, cursorWallet, cursorToken,
		upper.NetworkID, upper.WalletID, upper.TokenID, limit)
	if err != nil {
		return nil, fmt.Errorf("load reconciliation wallet page: %w", err)
	}
	defer rows.Close()

	items := make([]WorkerReconciliationWalletBalance, 0, limit)
	for rows.Next() {
		var item WorkerReconciliationWalletBalance
		if err := rows.Scan(
			&item.WalletID,
			&item.NetworkID,
			&item.Address,
			&item.TokenID,
			&item.ContractAddress,
			&item.TokenStandard,
			&item.TokenDecimals,
			&item.LedgerAmount,
		); err != nil {
			return nil, fmt.Errorf("scan reconciliation wallet: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *WorkerRuntimeRepo) StartReconciliationRun(ctx context.Context, workerID string, lease time.Duration) (id.ID, bool, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	if _, err := querier.Exec(ctx, `
		UPDATE reg_crypto_reconciliation_runs
		SET status = 'failed',
		    completed_at = NOW(),
		    error_message = CASE
		        WHEN error_message = '' THEN 'reconciliation worker lease expired'
		        ELSE error_message
		    END,
		    lease_expires_at = NULL
		WHERE status = 'running'
		  AND COALESCE(lease_expires_at, '-infinity'::timestamptz) < NOW()
	`); err != nil {
		return id.Nil(), false, fmt.Errorf("expire stale reconciliation run: %w", err)
	}

	var runID id.ID
	err := querier.QueryRow(ctx, `
		INSERT INTO reg_crypto_reconciliation_runs (
			status, started_at, worker_id, heartbeat_at, lease_expires_at
		) VALUES (
			'running', NOW(), $1, NOW(), NOW() + ($2 * interval '1 second')
		)
		ON CONFLICT DO NOTHING
		RETURNING id
	`, workerID, int64(lease.Seconds())).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return id.Nil(), false, nil
	}
	if err != nil {
		return id.Nil(), false, fmt.Errorf("create reconciliation run: %w", err)
	}
	return runID, true, nil
}

func (r *WorkerRuntimeRepo) HeartbeatReconciliationRun(ctx context.Context, runID id.ID, workerID string, lease time.Duration) error {
	txm := postgres.MustGetTxManager(ctx)
	command, err := txm.GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_reconciliation_runs
		SET heartbeat_at = NOW(),
		    lease_expires_at = NOW() + ($3 * interval '1 second')
		WHERE id = $1
		  AND worker_id = $2
		  AND status = 'running'
	`, runID, workerID, int64(lease.Seconds()))
	if err != nil {
		return fmt.Errorf("heartbeat reconciliation run: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("reconciliation run lease lost")
	}
	return nil
}

func (r *WorkerRuntimeRepo) FinishReconciliationRun(ctx context.Context, workerID string, result WorkerReconciliationStats, status, errorMessage string) error {
	txm := postgres.MustGetTxManager(ctx)
	command, err := txm.GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_reconciliation_runs
		SET status = $3,
		    completed_at = NOW(),
		    heartbeat_at = NOW(),
		    lease_expires_at = NULL,
		    wallets_checked = $4,
		    items_created = $5,
		    items_updated = $6,
		    items_resolved = $7,
		    read_errors = $8,
		    error_message = $9,
		    eligible_pairs = $10,
		    attempted_pairs = $11
		WHERE id = $1
		  AND worker_id = $2
		  AND status = 'running'
	`, result.RunID, workerID, status, result.WalletsChecked, result.ItemsCreated,
		result.ItemsUpdated, result.ItemsResolved, result.ReadErrors, errorMessage,
		result.EligiblePairs, result.AttemptedPairs)
	if err != nil {
		return fmt.Errorf("finish reconciliation run: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("finish reconciliation run: lease lost")
	}
	return nil
}

// InFlightInbound sums incoming transfers to the wallet that the chain already
// shows but the custody ledger has not recognized yet: no custody receipt and
// no posted payment. Orphaned and ignored transfers are excluded.
func (r *WorkerRuntimeRepo) InFlightInbound(ctx context.Context, walletID, tokenID id.ID) (types.RawAmount, error) {
	var raw string
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT COALESCE(SUM(e.amount),0)::text
		FROM reg_crypto_inbound_chain_events e
		WHERE e.wallet_id=$1 AND e.token_id=$2 AND e._deleted_at IS NULL
		  AND e.direction IN ('incoming','') AND e.canonicality<>'orphaned'
		  AND e.match_status IN ('received','matched','unmatched','needs_review')
		  AND e.receipt_id IS NULL
		  AND NOT EXISTS (SELECT 1 FROM doc_crypto_payments p WHERE p.id=e.payment_id AND p.posted)`,
		walletID, tokenID).Scan(&raw)
	if err != nil {
		return types.ZeroRawAmount(), fmt.Errorf("sum in-flight inbound transfers: %w", err)
	}
	return types.NewRawAmountFromString(raw)
}
