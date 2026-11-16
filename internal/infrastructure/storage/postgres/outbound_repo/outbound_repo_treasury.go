package outbound_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/treasury_withdrawal"
	"metapus/internal/domain/registers/crypto_fee"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

func (r *OutboundRepo) ClaimApprovedTreasuryWithdrawals(ctx context.Context, limit int) ([]TreasuryTask, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, `
		WITH picked AS (
			SELECT id
			FROM doc_treasury_withdrawals
			WHERE (status IN ('approved','signing') OR (status='held' AND EXISTS(
                    SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.basis_type='TreasuryWithdrawal' AND a.basis_id=doc_treasury_withdrawals.id AND a.signed_envelope IS NOT NULL)))
			  AND tx_hash = ''
			  AND _deleted_at IS NULL
			ORDER BY COALESCE(approved_at, updated_at), id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		),
		updated AS (
			UPDATE doc_treasury_withdrawals tw
			SET status = CASE WHEN tw.status='held' THEN 'held' ELSE 'signing' END,
			    updated_at = NOW(),
			    version = version + 1
			FROM picked
			WHERE tw.id = picked.id
			RETURNING tw.id, tw.token_id, tw.source_wallet_id, tw.amount, tw.dest_address, tw.posted_version,tw.status
		)
		SELECT u.id,
		       u.token_id,
		       u.source_wallet_id,
		       t.network_id,
		       COALESCE(n.code, '') AS network_code,
		       u.amount,
		       u.dest_address,
		       COALESCE(t.contract_address, '') AS contract_address,
		       t.token_standard,
		       t.decimal_places,
		       COALESCE(n.confirmations_needed, 1) AS required_confs,
		       u.posted_version,u.status='held'
		FROM updated u
		JOIN cat_tokens t ON t.id = u.token_id
		JOIN cat_blockchain_networks n ON n.id = t.network_id
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim approved treasury withdrawals: %w", err)
	}
	defer rows.Close()

	tasks := make([]TreasuryTask, 0, limit)
	for rows.Next() {
		var task TreasuryTask
		if err := rows.Scan(
			&task.WithdrawalID,
			&task.TokenID,
			&task.SourceWalletID,
			&task.NetworkID,
			&task.NetworkCode,
			&task.Amount,
			&task.DestAddress,
			&task.ContractAddress,
			&task.TokenStandard,
			&task.TokenDecimals,
			&task.RequiredConfs,
			&task.PostedVersion,
			&task.RecoveryOnly,
		); err != nil {
			return nil, fmt.Errorf("scan treasury task: %w", err)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// ReserveHotWalletForTreasury reserves the exact source wallet selected on the treasury document.
func (r *OutboundRepo) ReserveHotWalletForTreasury(ctx context.Context, task TreasuryTask) (HotWallet, error) {
	txm := postgres.MustGetTxManager(ctx)

	var selected HotWallet
	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := postgres.NewDocLocker().LockDocument(txCtx, treasury_withdrawal.DocumentType, task.WithdrawalID); err != nil {
			return err
		}
		querier := txm.GetQuerier(txCtx)
		var actualSource, actualToken, actualNetwork id.ID
		var actualAmount types.RawAmount
		if err := querier.QueryRow(txCtx, `SELECT tw.source_wallet_id,tw.token_id,t.network_id,tw.amount FROM doc_treasury_withdrawals tw
            JOIN cat_tokens t ON t.id=tw.token_id WHERE tw.id=$1`, task.WithdrawalID).Scan(&actualSource, &actualToken, &actualNetwork, &actualAmount); err != nil {
			return err
		}
		if actualSource != task.SourceWalletID || actualToken != task.TokenID || actualNetwork != task.NetworkID || !actualAmount.Equal(task.Amount) {
			return holdExecution(fmt.Errorf("%w: treasury request identity mismatch", ErrMerchantCustody))
		}
		if _, err := querier.Exec(txCtx, `SELECT fn_crypto_lock_custody_wallets(ARRAY[$1::uuid])`, actualSource); err != nil {
			return err
		}
		var current bool
		if err := querier.QueryRow(txCtx, `SELECT source_wallet_id=$2 AND token_id=$3 AND amount=$4 FROM doc_treasury_withdrawals WHERE id=$1 FOR UPDATE`, task.WithdrawalID, actualSource, actualToken, actualAmount).Scan(&current); err != nil {
			return err
		}
		if !current {
			return holdExecution(fmt.Errorf("%w: treasury request identity changed", ErrMerchantCustody))
		}
		var reservedToken id.ID
		var reservedAmount types.RawAmount

		err := querier.QueryRow(txCtx, `
			SELECT w.id, w.address, w.vault_key_id, COALESCE(b.amount, 0) AS balance,r.token_id,r.amount
			FROM reg_crypto_balance_reservations r
			JOIN cat_wallets w ON w.id = r.wallet_id
			LEFT JOIN reg_crypto_balance_balances b
			       ON b.wallet_id = w.id AND b.token_id = r.token_id
			WHERE r.document_type = $2
			  AND r.document_id = $1
			  AND r.status = 'active'
			LIMIT 1
		`, task.WithdrawalID, treasury_withdrawal.DocumentType).Scan(&selected.ID, &selected.Address, &selected.VaultKeyID, &selected.Balance, &reservedToken, &reservedAmount)
		if err == nil {
			source, scopeErr := loadCustodyWallet(txCtx, selected.ID)
			if scopeErr != nil {
				return scopeErr
			}
			enforced, scopeErr := merchantCustodyEnforced(txCtx, task.NetworkID)
			if scopeErr != nil {
				return scopeErr
			}
			if source.MerchantID != nil || source.ID != task.SourceWalletID || source.NetworkID != task.NetworkID || reservedToken != task.TokenID || !reservedAmount.Equal(task.Amount) || (source.Role != "treasury" && (enforced || source.Role != "liquidity_hot")) {
				return holdExecution(fmt.Errorf("%w: existing treasury reservation has a different custody scope", ErrMerchantCustody))
			}
			if selected.VaultKeyID == "" {
				return fmt.Errorf("reserved treasury wallet has empty vault_key_id")
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("find existing treasury wallet reservation: %w", err)
		}

		err = querier.QueryRow(txCtx, `
			WITH reserved AS (
				SELECT wallet_id, token_id, SUM(amount) AS amount
				FROM reg_crypto_balance_reservations
				WHERE status = 'active'
				GROUP BY wallet_id, token_id
			),
			candidate AS (
				SELECT w.id,
				       w.address,
				       w.vault_key_id,
				       b.amount,
				       b.amount - COALESCE(r.amount, 0) AS available
				FROM cat_wallets w
				JOIN reg_crypto_balance_balances b
				  ON b.wallet_id = w.id AND b.token_id = $2
				LEFT JOIN reserved r
				  ON r.wallet_id = w.id AND r.token_id = b.token_id
				WHERE w.id = $1
				  AND w.network_id = $3
				  AND w.tier = $4
				  AND w.custody_merchant_id IS NULL
                  AND (w.wallet_role='treasury' OR (w.wallet_role='liquidity_hot' AND NOT EXISTS(
                      SELECT 1 FROM sys_crypto_custody_policies cp WHERE cp.network_id=w.network_id AND cp.enforced)))
				  AND w.is_active = TRUE
				  AND w.deletion_mark = FALSE
				  AND w.vault_key_id <> ''
				  AND b.amount - COALESCE(r.amount, 0) >= $5
				FOR UPDATE OF b
			),
			inserted AS (
				INSERT INTO reg_crypto_balance_reservations (
					wallet_id, token_id, amount, purpose, document_type, document_id
				)
				SELECT id, $2, $5, 'treasury_withdrawal', $7, $6
				FROM candidate
				RETURNING wallet_id
			)
			SELECT c.id, c.address, c.vault_key_id, c.amount
			FROM candidate c
			JOIN inserted i ON i.wallet_id = c.id
		`, task.SourceWalletID, task.TokenID, task.NetworkID, wallet.WalletTierHot, task.Amount, task.WithdrawalID, treasury_withdrawal.DocumentType).Scan(
			&selected.ID,
			&selected.Address,
			&selected.VaultKeyID,
			&selected.Balance,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoHotWalletLiquidity
		}
		if err != nil {
			return fmt.Errorf("reserve treasury hot wallet: %w", err)
		}
		return nil
	})
	if err != nil {
		return HotWallet{}, err
	}
	return selected, nil
}

// ReserveFeeForTreasury creates deterministic platform-revenue reservations for a treasury withdrawal.
func (r *OutboundRepo) ReserveFeeForTreasury(ctx context.Context, withdrawalID, tokenID id.ID, amount types.RawAmount) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := postgres.NewDocLocker().LockDocument(txCtx, treasury_withdrawal.DocumentType, withdrawalID); err != nil {
			return err
		}
		querier := txm.GetQuerier(txCtx)
		refunds := crypto_repo.NewReceiptRefundRepo()
		refund, err := refunds.IsReceiptRefund(txCtx, withdrawalID)
		if err != nil {
			return err
		}
		if refund {
			return refunds.OpenPayable(txCtx, withdrawalID)
		}
		if _, err := querier.Exec(txCtx, `SELECT merchant_id FROM reg_crypto_fee_balances WHERE token_id=$1 ORDER BY merchant_id,fee_type FOR UPDATE`, tokenID); err != nil {
			return err
		}

		var existing types.RawAmount
		if err := querier.QueryRow(txCtx, `
			SELECT COALESCE(SUM(amount), 0)
			FROM reg_crypto_revenue_reservations
			WHERE document_type = $2
			  AND document_id = $1
			  AND status IN ('active','consumed')
		`, withdrawalID, treasury_withdrawal.DocumentType).Scan(&existing); err != nil {
			return fmt.Errorf("sum existing treasury fee reservations: %w", err)
		}
		if existing.Equal(amount) {
			return nil
		}
		if !existing.IsZero() {
			return fmt.Errorf("treasury fee reservations conflict: existing=%s requested=%s", existing, amount)
		}

		rows, err := querier.Query(txCtx, `
			WITH reserved AS (
				SELECT merchant_id, token_id, fee_type, SUM(amount) AS amount
				FROM reg_crypto_revenue_reservations
				WHERE status = 'active'
				GROUP BY merchant_id, token_id, fee_type
			)
			SELECT b.merchant_id,
			       b.token_id,
			       b.fee_type,
			       b.balance - COALESCE(r.amount, 0) AS available
			FROM reg_crypto_fee_balances b
			LEFT JOIN reserved r
			  ON r.merchant_id = b.merchant_id
			 AND r.token_id = b.token_id
			 AND r.fee_type = b.fee_type
			WHERE b.token_id = $1
			  AND b.balance - COALESCE(r.amount, 0) > 0
			ORDER BY b.merchant_id, b.fee_type
			FOR UPDATE OF b
		`, tokenID)
		if err != nil {
			return fmt.Errorf("select available fee balances: %w", err)
		}
		defer rows.Close()

		remaining := amount
		type feeRow struct {
			merchantID id.ID
			tokenID    id.ID
			feeType    crypto_fee.FeeType
			available  types.RawAmount
		}
		selected := make([]feeRow, 0, 4)
		for rows.Next() {
			var row feeRow
			if err := rows.Scan(&row.merchantID, &row.tokenID, &row.feeType, &row.available); err != nil {
				return fmt.Errorf("scan fee balance: %w", err)
			}
			if !row.available.IsPositive() || !remaining.IsPositive() {
				continue
			}
			take := row.available
			if take.Cmp(remaining) > 0 {
				take = remaining
			}
			selected = append(selected, feeRow{
				merchantID: row.merchantID,
				tokenID:    row.tokenID,
				feeType:    row.feeType,
				available:  take,
			})
			remaining = remaining.Sub(take)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate fee balances: %w", err)
		}
		if remaining.IsPositive() {
			return fmt.Errorf("insufficient crypto platform revenue")
		}

		for _, row := range selected {
			if _, err := querier.Exec(txCtx, `
				INSERT INTO reg_crypto_revenue_reservations (
					merchant_id, token_id, fee_type, amount, purpose, document_type, document_id
				) VALUES ($1, $2, $3, $4, 'treasury_withdrawal', $6, $5)
			`, row.merchantID, row.tokenID, row.feeType, row.available, withdrawalID, treasury_withdrawal.DocumentType); err != nil {
				return fmt.Errorf("insert fee reservation: %w", err)
			}
		}
		return nil
	})
}

// LoadTreasuryFeeReservationLines returns fee reservation lines for posting.
func (r *OutboundRepo) LoadTreasuryFeeReservationLines(ctx context.Context, withdrawalID id.ID) ([]treasury_withdrawal.FeeLine, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, `
		SELECT merchant_id, token_id, fee_type, amount
		FROM reg_crypto_revenue_reservations
		WHERE document_type = $2
		  AND document_id = $1
		  AND status IN ('active','consumed')
		ORDER BY merchant_id, fee_type, id
	`, withdrawalID, treasury_withdrawal.DocumentType)
	if err != nil {
		return nil, fmt.Errorf("load treasury fee reservations: %w", err)
	}
	defer rows.Close()

	lines := make([]treasury_withdrawal.FeeLine, 0, 4)
	for rows.Next() {
		var line treasury_withdrawal.FeeLine
		if err := rows.Scan(&line.MerchantID, &line.TokenID, &line.FeeType, &line.Amount); err != nil {
			return nil, fmt.Errorf("scan treasury fee reservation: %w", err)
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

// ConsumeTreasuryReservations marks active wallet and fee reservations consumed.
func (r *OutboundRepo) ConsumeTreasuryReservations(ctx context.Context, withdrawalID id.ID) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	if tag, err := querier.Exec(ctx, `
		UPDATE reg_crypto_balance_reservations
		SET status = 'consumed',
		    updated_at = NOW()
		WHERE document_type = $2
		  AND document_id = $1
		  AND status = 'active'
	`, withdrawalID, treasury_withdrawal.DocumentType); err != nil {
		return fmt.Errorf("consume treasury wallet reservation: %w", err)
	} else if tag.RowsAffected() == 0 {
		return fmt.Errorf("active treasury wallet reservation not found")
	}
	// A receipt refund reserves no revenue; its payable is closed with the payout.
	if refund, err := crypto_repo.NewReceiptRefundRepo().IsReceiptRefund(ctx, withdrawalID); err != nil || refund {
		return err
	}

	if tag, err := querier.Exec(ctx, `
		UPDATE reg_crypto_revenue_reservations
		SET status = 'consumed',
		    updated_at = NOW()
		WHERE document_type = $2
		  AND document_id = $1
		  AND status = 'active'
	`, withdrawalID, treasury_withdrawal.DocumentType); err != nil {
		return fmt.Errorf("consume treasury fee reservations: %w", err)
	} else if tag.RowsAffected() == 0 {
		return fmt.Errorf("active treasury fee reservations not found")
	}

	return nil
}

// ReleaseTreasuryReservations releases active wallet and fee reservations.
func (r *OutboundRepo) ReleaseTreasuryReservations(ctx context.Context, withdrawalID id.ID) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	if _, err := querier.Exec(ctx, `
		UPDATE reg_crypto_balance_reservations
		SET status = 'released',
		    updated_at = NOW()
		WHERE document_type = $2
		  AND document_id = $1
		  AND status = 'active'
	`, withdrawalID, treasury_withdrawal.DocumentType); err != nil {
		return fmt.Errorf("release treasury wallet reservation: %w", err)
	}
	if _, err := querier.Exec(ctx, `
		UPDATE reg_crypto_revenue_reservations
		SET status = 'released',
		    updated_at = NOW()
		WHERE document_type = $2
		  AND document_id = $1
		  AND status = 'active'
	`, withdrawalID, treasury_withdrawal.DocumentType); err != nil {
		return fmt.Errorf("release treasury fee reservations: %w", err)
	}
	refunds := crypto_repo.NewReceiptRefundRepo()
	if refund, err := refunds.IsReceiptRefund(ctx, withdrawalID); err != nil || !refund {
		return err
	}
	return refunds.Restore(ctx, withdrawalID)
}

// GetTreasurySigningAttempt loads the existing immutable treasury signing attempt.
func (r *OutboundRepo) GetTreasurySigningAttempt(ctx context.Context, withdrawalID id.ID) (HotWallet, SigningAuthorizationRecord, bool, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	var selected HotWallet
	var record SigningAuthorizationRecord
	err := r.scanSigningAttempt(querier.QueryRow(ctx, `
		SELECT w.id,
		       a.source_address,
		       w.vault_key_id,
		       COALESCE(b.amount, 0) AS balance,`+signingAttemptSelectColumns+`
		FROM reg_crypto_signing_authorizations a
		JOIN reg_crypto_balance_reservations rsv
		  ON rsv.document_type = $2
		 AND rsv.document_id = a.basis_id
		 AND rsv.status IN ('active','consumed')
		JOIN cat_wallets w
		  ON w.id = rsv.wallet_id
		 AND w.address = a.source_address
		 AND w.vault_key_id = a.vault_key_id
		LEFT JOIN doc_treasury_withdrawals tw ON tw.id = a.basis_id
		LEFT JOIN reg_crypto_balance_balances b
		  ON b.wallet_id = w.id AND b.token_id = tw.token_id
		WHERE a.basis_type = $2
		  AND a.basis_id = $1
		  AND a.attempt = 1
		ORDER BY a.created_at DESC
		LIMIT 1
	`, withdrawalID, treasury_withdrawal.DocumentType), &selected, &record)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return HotWallet{}, SigningAuthorizationRecord{}, false, nil
		}
		return HotWallet{}, SigningAuthorizationRecord{}, false, err
	}
	return selected, record, true, nil
}

// MarkTreasuryHeld marks a treasury withdrawal held for manual reconciliation.
func (r *OutboundRepo) MarkTreasuryHeld(ctx context.Context, withdrawalID id.ID, reason string) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	tag, err := querier.Exec(ctx, `
		UPDATE doc_treasury_withdrawals
		SET status = 'held',
		    held_reason = $2,
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = $1
		  AND status IN ('approved','signing','held')
		  AND _deleted_at IS NULL
	`, withdrawalID, statusFailedReason(errors.New(reason)))
	if err != nil {
		return fmt.Errorf("mark treasury held: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("treasury withdrawal cannot be marked held")
	}
	return nil
}

// MarkTreasuryFailed marks a treasury withdrawal failed after safe reservation release.
func (r *OutboundRepo) MarkTreasuryFailed(ctx context.Context, withdrawalID id.ID, reason string) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	tag, err := querier.Exec(ctx, `
		UPDATE doc_treasury_withdrawals
		SET status = 'failed',
		    rejection_reason = $2,
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = $1
		  AND status IN ('approved','signing')
		  AND _deleted_at IS NULL
	`, withdrawalID, reason)
	if err != nil {
		return fmt.Errorf("mark treasury failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("treasury withdrawal cannot be marked failed")
	}
	return nil
}

// RequeueTreasuryWithdrawal moves a claimed treasury withdrawal back to approved.
func (r *OutboundRepo) RequeueTreasuryWithdrawal(ctx context.Context, withdrawalID id.ID, reason string) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	tag, err := querier.Exec(ctx, `
		UPDATE doc_treasury_withdrawals
		SET status = 'approved',
		    held_reason = $2,
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = $1
		  AND status = 'signing'
		  AND tx_hash = ''
		  AND _deleted_at IS NULL
	`, withdrawalID, reason)
	if err != nil {
		return fmt.Errorf("requeue treasury withdrawal: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("treasury withdrawal cannot be requeued")
	}
	return nil
}

// ListBroadcastTreasuryWithdrawals returns treasury withdrawals awaiting confirmations.
func (r *OutboundRepo) ListBroadcastTreasuryWithdrawals(ctx context.Context, limit int) ([]BroadcastTreasuryWithdrawal, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, `
		SELECT tw.id,
		       t.network_id,
		       tw.tx_hash,
		       COALESCE(n.confirmations_needed, 1) AS required_confs
		FROM doc_treasury_withdrawals tw
		JOIN cat_tokens t ON t.id = tw.token_id
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE tw.status = 'broadcast'
		  AND tw.tx_hash <> ''
		  AND tw._deleted_at IS NULL
		ORDER BY tw.updated_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list broadcast treasury withdrawals: %w", err)
	}
	defer rows.Close()

	items := make([]BroadcastTreasuryWithdrawal, 0, limit)
	for rows.Next() {
		var item BroadcastTreasuryWithdrawal
		if err := rows.Scan(&item.WithdrawalID, &item.NetworkID, &item.TxHash, &item.RequiredConfs); err != nil {
			return nil, fmt.Errorf("scan broadcast treasury withdrawal: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// MarkTreasuryConfirmed finalizes a broadcast treasury withdrawal.
func (r *OutboundRepo) MarkTreasuryConfirmed(ctx context.Context, withdrawalID id.ID) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	tag, err := querier.Exec(ctx, `
		UPDATE doc_treasury_withdrawals
		SET status = 'confirmed',
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = $1
		  AND status = 'broadcast'
		  AND _deleted_at IS NULL
	`, withdrawalID)
	if err != nil {
		return fmt.Errorf("confirm treasury withdrawal: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("treasury withdrawal cannot be confirmed")
	}
	return nil
}

// GetTokenRequiredConfirmations returns required confirmations for the token network.
func (r *OutboundRepo) GetTokenRequiredConfirmations(ctx context.Context, tokenID id.ID) (int, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	var required int
	if err := querier.QueryRow(ctx, `
		SELECT COALESCE(n.confirmations_needed, 1)
		FROM cat_tokens t
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE t.id = $1
	`, tokenID).Scan(&required); err != nil {
		return 0, fmt.Errorf("get token confirmations: %w", err)
	}
	return required, nil
}
