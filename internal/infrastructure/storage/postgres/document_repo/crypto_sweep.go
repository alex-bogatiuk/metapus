package document_repo

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/infrastructure/storage/postgres"
)

const (
	_cryptoSweepLinesTable = "doc_crypto_sweep_lines"
)

// CryptoSweepRepo implements crypto_sweep.Repository.
type CryptoSweepRepo struct {
	*BaseDocumentRepo[*crypto_sweep.CryptoSweep]
}

// NewCryptoSweepRepo creates a new crypto sweep repository.
func NewCryptoSweepRepo() *CryptoSweepRepo {
	repo := &CryptoSweepRepo{
		BaseDocumentRepo: NewDocumentRepoForDefinition[*crypto_sweep.CryptoSweep](crypto_sweep.Definition,
			postgres.ExtractDBColumns[crypto_sweep.CryptoSweep](),
			func() *crypto_sweep.CryptoSweep { return &crypto_sweep.CryptoSweep{} },
		),
	}
	// System document — no RLS dimensions (admin-only)
	return repo
}

// GetLines retrieves lines for a crypto sweep.
func (r *CryptoSweepRepo) GetLines(ctx context.Context, docID id.ID) ([]crypto_sweep.CryptoSweepLine, error) {
	q := r.Builder().
		Select("line_id", "line_no", "wallet_id", "amount", "network_fee", "tx_hash", "confirmed",
			"network_fee_token_id", "settlement_status", "settlement_checked_at", "settlement_error",
			"settlement_claimed_by", "settlement_claimed_at", "settlement_lease_expires_at",
			"chain_result", "energy_used", "net_fee", "energy_fee", "net_usage").
		From(_cryptoSweepLinesTable).
		Where(squirrel.Eq{"document_id": docID}).
		OrderBy("line_no")

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var lines []crypto_sweep.CryptoSweepLine
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &lines, sql, args...); err != nil {
		return nil, fmt.Errorf("get lines: %w", err)
	}
	return lines, nil
}

// SaveLines saves lines for a crypto sweep.
func (r *CryptoSweepRepo) SaveLines(ctx context.Context, docID id.ID, lines []crypto_sweep.CryptoSweepLine) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	deleteSQL := "DELETE FROM " + _cryptoSweepLinesTable + " WHERE document_id = $1"
	if _, err := querier.Exec(ctx, deleteSQL, docID); err != nil {
		return fmt.Errorf("delete existing lines: %w", err)
	}
	if len(lines) == 0 {
		return nil
	}

	columns := []string{"line_id", "document_id", "line_no", "wallet_id", "amount", "network_fee", "tx_hash", "confirmed",
		"network_fee_token_id", "settlement_status", "settlement_checked_at", "settlement_error",
		"settlement_claimed_by", "settlement_claimed_at", "settlement_lease_expires_at",
		"chain_result", "energy_used", "net_fee", "energy_fee", "net_usage"}
	rows := make([][]any, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, []any{
			line.LineID, docID, line.LineNo, line.WalletID,
			line.Amount, line.NetworkFee, line.TxHash, line.Confirmed,
			line.NetworkFeeTokenID, line.SettlementStatus.String(), line.SettlementCheckedAt, line.SettlementError,
			line.SettlementClaimedBy, line.SettlementClaimedAt, line.SettlementLeaseExpiresAt,
			line.ChainResult.String(), line.EnergyUsed, line.NetFee, line.EnergyFee, line.NetUsage,
		})
	}

	txm := r.getTxManager(ctx)
	inserter := postgres.NewBatchInserter(txm)
	if _, err := inserter.CopyFromSlice(ctx, _cryptoSweepLinesTable, columns, rows); err != nil {
		return fmt.Errorf("copy lines: %w", err)
	}
	return nil
}

// ListByStatus returns sweeps with the specified lifecycle status and loads their lines.
func (r *CryptoSweepRepo) ListByStatus(ctx context.Context, status crypto_sweep.SweepStatus, limit int) ([]*crypto_sweep.CryptoSweep, error) {
	q := r.Builder().Select("*").
		From(crypto_sweep.Definition.Table).
		Where(squirrel.Eq{"status": status}).
		Where(squirrel.Eq{"deletion_mark": false}).
		OrderBy("updated_at ASC").
		Limit(uint64(limit))

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	var sweeps []*crypto_sweep.CryptoSweep
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if err := pgxscan.Select(ctx, querier, &sweeps, sql, args...); err != nil {
		return nil, fmt.Errorf("list sweeps by status: %w", err)
	}

	for _, sweep := range sweeps {
		lines, err := r.GetLines(ctx, sweep.ID)
		if err != nil {
			return nil, fmt.Errorf("load sweep lines: %w", err)
		}
		sweep.SetLines(lines)
	}

	return sweeps, nil
}

// RetryHeldSweep re-queues an unsigned held sweep document and its attempts for execution.
func (r *CryptoSweepRepo) RetryHeldSweep(ctx context.Context, docID, actorID id.ID) (bool, error) {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	var exists int
	if err := querier.QueryRow(ctx, `
		SELECT 1
		FROM doc_crypto_sweeps
		WHERE id = $1
		  AND status = 'held'
		  AND posted = FALSE
		  AND deletion_mark = FALSE
		FOR UPDATE
	`, docID).Scan(&exists); err != nil {
		if pgxscan.NotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("lock held sweep: %w", err)
	}

	if _, err := querier.Exec(ctx, `
		UPDATE reg_crypto_fee_reservations f
		SET status = 'released',
		    released_at = NOW(),
		    hold_reason = ''
		FROM reg_crypto_sweep_attempts a
		WHERE a.sweep_id = $1
		  AND a.status = 'held'
		  AND a.broadcast_at IS NULL
		  AND f.basis_type = 'CryptoSweepAttempt'
		  AND f.basis_id = a.id
		  AND f.tx_hash = ''
		  AND f.status IN ('reserved', 'held')
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_signing_authorizations s
		      WHERE s.basis_type = 'CryptoSweepAttempt' AND s.basis_id = a.id
		  )
	`, docID); err != nil {
		return false, fmt.Errorf("release unbroadcast fee reservations: %w", err)
	}

	if _, err := querier.Exec(ctx, `
		UPDATE doc_crypto_resource_rentals
		SET status = 'ordering',
		    error_message = '',
		    claimed_by = '',
		    claimed_at = NULL,
		    lease_expires_at = NULL,
		    updated_by = $2,
		    updated_at = NOW(),
		    version = version + 1
		WHERE basis_type = 'CryptoSweep'
		  AND basis_id = $1
		  AND status = 'held'
		  AND external_serial = ''
		  AND deletion_mark = FALSE
	`, docID, actorID); err != nil {
		return false, fmt.Errorf("reset pre-provider resource rentals: %w", err)
	}

	var walletIDs []id.ID
	if err := pgxscan.Select(ctx, querier, &walletIDs, `
		UPDATE reg_crypto_sweep_attempts a
		SET status = 'claimed',
		    error_message = '',
		    claimed_at = NOW(),
		    expires_at = GREATEST(a.expires_at, NOW() + interval '10 minutes'),
		    updated_at = NOW()
		WHERE a.sweep_id = $1
		  AND a.status = 'held'
		  AND a.broadcast_at IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM doc_crypto_sweep_lines l
		      WHERE l.line_id = a.id AND l.tx_hash <> ''
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_signing_authorizations s
		      WHERE s.basis_type = 'CryptoSweepAttempt' AND s.basis_id = a.id
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM reg_crypto_fee_reservations f
		      WHERE f.basis_type = 'CryptoSweepAttempt' AND f.basis_id = a.id AND f.tx_hash <> ''
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM doc_crypto_resource_rentals r
		      WHERE r.basis_type = 'CryptoSweep' AND r.basis_id = $1
		        AND r.receive_wallet_id = a.wallet_id AND r.deletion_mark = FALSE
		        AND r.status = 'held' AND r.external_serial <> ''
		  )
		RETURNING a.wallet_id
	`, docID); err != nil {
		return false, fmt.Errorf("reset held sweep attempts: %w", err)
	}
	if len(walletIDs) == 0 {
		return false, nil
	}

	if _, err := querier.Exec(ctx, `
		UPDATE cat_wallets
		SET status = 'sweeping'
		WHERE id = ANY($1::uuid[])
		  AND status IN ('free', 'sweep_pending', 'sweeping')
	`, walletIDs); err != nil {
		return false, fmt.Errorf("restore sweeping wallet status: %w", err)
	}

	tag, err := querier.Exec(ctx, `
		UPDATE doc_crypto_sweeps
		SET status = 'created',
		    updated_by = $2,
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = $1
		  AND status = 'held'
		  AND posted = FALSE
		  AND deletion_mark = FALSE
	`, docID, actorID)
	if err != nil {
		return false, fmt.Errorf("update sweep status to created: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
