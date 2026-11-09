package outbound_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

// ClaimApprovedWithdrawals atomically claims approved requests for worker execution.
func (r *OutboundRepo) ClaimApprovedWithdrawals(ctx context.Context, limit int) ([]WithdrawalTask, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)
	if _, err := querier.Exec(ctx, `
		UPDATE doc_withdrawal_requests wr
		SET status = 'held',
		    rejection_reason = $1,
		    updated_at = NOW(),
		    version = wr.version + 1
		FROM cat_tokens t
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE wr.token_id = t.id
		  AND wr.status IN ('approved','signing')
		  AND wr._deleted_at IS NULL
		  AND EXISTS (
		      SELECT 1
		      FROM cat_wallets w
		      WHERE w.network_id = t.network_id
		        AND w.deletion_mark = FALSE
		        AND (
		            w.address = wr.dest_address
		            OR (n.chain_family = 'evm' AND LOWER(w.address) = LOWER(wr.dest_address))
		        )
		  )
	`, ErrManagedWithdrawalDestination.Error()); err != nil {
		return nil, fmt.Errorf("hold managed withdrawal destinations: %w", err)
	}

	rows, err := querier.Query(ctx, `
		WITH picked AS (
			SELECT wr.id
			FROM doc_withdrawal_requests wr
			JOIN cat_tokens t ON t.id = wr.token_id
			JOIN cat_blockchain_networks n ON n.id = t.network_id
			WHERE (wr.status IN ('approved','signing') OR
			       (wr.status='held' AND EXISTS (SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.basis_type='WithdrawalRequest' AND a.basis_id=wr.id AND a.signed_envelope IS NOT NULL)))
			  AND (wr.status = 'approved' OR wr.updated_at < $2)
			  AND wr._deleted_at IS NULL
              AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_plans aml_plan WHERE aml_plan.request_id=wr.id)
			  AND (EXISTS (SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.basis_type='WithdrawalRequest' AND a.basis_id=wr.id AND a.signed_envelope IS NOT NULL) OR NOT EXISTS (
			      SELECT 1
			      FROM cat_wallets w
			      WHERE w.network_id = t.network_id
			        AND w.deletion_mark = FALSE
			        AND (
			            w.address = wr.dest_address
			            OR (n.chain_family = 'evm' AND LOWER(w.address) = LOWER(wr.dest_address))
			        )
			  ))
			ORDER BY COALESCE(wr.approved_at, wr.updated_at), wr.id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		),
		updated AS (
			UPDATE doc_withdrawal_requests wr
			SET status = CASE WHEN wr.status='held' THEN 'held' ELSE 'signing' END,
			    updated_at = NOW(),
			    version = version + 1
			FROM picked
			WHERE wr.id = picked.id
			RETURNING wr.id, wr.merchant_id, wr.token_id, wr.amount, wr.dest_address, wr.posted_version, wr.status
		)
		SELECT u.id,
		       u.merchant_id,
		       u.token_id,
		       t.network_id,
		       COALESCE(n.code, '') AS network_code,
		       u.amount,
		       u.dest_address,
		       COALESCE(t.contract_address, '') AS contract_address,
		       t.token_standard,
		       t.decimal_places,
		       COALESCE(n.confirmations_needed, 1) AS required_confs,
		       u.posted_version, u.status='held'
		FROM updated u
		JOIN cat_tokens t ON t.id = u.token_id
		JOIN cat_blockchain_networks n ON n.id = t.network_id
	`, limit, time.Now().UTC().Add(-_withdrawalExecutionRetryAge))
	if err != nil {
		return nil, fmt.Errorf("claim approved withdrawals: %w", err)
	}
	defer rows.Close()

	tasks := make([]WithdrawalTask, 0, limit)
	for rows.Next() {
		var task WithdrawalTask
		if err := rows.Scan(
			&task.RequestID,
			&task.MerchantID,
			&task.TokenID,
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
			return nil, fmt.Errorf("scan withdrawal task: %w", err)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// ValidateExternalWithdrawalDestination rechecks the custody boundary at the
// last internal step before a transaction can be built and signed.
func (r *OutboundRepo) ValidateExternalWithdrawalDestination(
	ctx context.Context,
	networkID id.ID,
	destination string,
) error {
	txm := postgres.MustGetTxManager(ctx)
	normalized, err := crypto_repo.NewChainAddressPolicy().ValidateAndNormalize(ctx, networkID, destination)
	if err != nil {
		return err
	}
	var managed bool
	if err := txm.GetQuerier(ctx).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM cat_wallets w
			WHERE w.network_id = $1
			  AND w.deletion_mark = FALSE
			  AND w.address_key = $2
		)
	`, networkID, normalized.CanonicalKey).Scan(&managed); err != nil {
		return fmt.Errorf("validate withdrawal destination custody boundary: %w", err)
	}
	if managed {
		return ErrManagedWithdrawalDestination
	}
	return nil
}

// EnsureWithdrawalExecutionDocument creates and links the unposted execution
// document before any external resource order or blockchain signing occurs.
func (r *OutboundRepo) EnsureWithdrawalExecutionDocument(
	ctx context.Context,
	requestID id.ID,
	candidate *crypto_withdrawal.CryptoWithdrawal,
	repo crypto_withdrawal.Repository,
) (*crypto_withdrawal.CryptoWithdrawal, error) {
	if candidate == nil || candidate.BasisID == nil || *candidate.BasisID != requestID {
		return nil, fmt.Errorf("withdrawal execution draft has invalid basis")
	}
	txm := postgres.MustGetTxManager(ctx)
	var result *crypto_withdrawal.CryptoWithdrawal
	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		var existingID *id.ID
		if err := q.QueryRow(txCtx, `
			SELECT withdrawal_id
			FROM doc_withdrawal_requests
			WHERE id=$1 AND status IN ('approved','signing','broadcast') AND _deleted_at IS NULL
			FOR UPDATE
		`, requestID).Scan(&existingID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("withdrawal request not found or not executable")
			}
			return err
		}
		if existingID != nil {
			existing, err := repo.GetByID(txCtx, *existingID)
			if err != nil {
				return err
			}
			result = existing
			return nil
		}
		if err := repo.Create(txCtx, candidate); err != nil {
			return fmt.Errorf("create withdrawal execution draft: %w", err)
		}
		tag, err := q.Exec(txCtx, `
			UPDATE doc_withdrawal_requests
			SET withdrawal_id=$2,updated_at=NOW(),version=version+1
			WHERE id=$1 AND withdrawal_id IS NULL AND status IN ('approved','signing')
		`, requestID, candidate.ID)
		if err != nil {
			return fmt.Errorf("link withdrawal execution draft: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("withdrawal execution draft could not be linked")
		}
		result = candidate
		return nil
	})
	return result, err
}

func (r *OutboundRepo) MarkWithdrawalExecutionSigned(ctx context.Context, withdrawalID id.ID) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	tag, err := q.Exec(ctx, `
		UPDATE doc_crypto_withdrawals
		SET status='signed',updated_at=NOW()
		WHERE id=$1 AND status IN ('created','signed') AND posted=FALSE AND deletion_mark=FALSE
	`, withdrawalID)
	if err != nil {
		return fmt.Errorf("mark withdrawal execution signed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("withdrawal execution document cannot be marked signed")
	}
	return nil
}

func (r *OutboundRepo) MarkWithdrawalExecutionFailed(ctx context.Context, requestID id.ID, reason string) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	_, err := q.Exec(ctx, `
		UPDATE doc_crypto_withdrawals cw
		SET status=$3,settlement_status=$4,settlement_error=$2,updated_at=NOW()
		FROM doc_withdrawal_requests wr
		WHERE wr.id=$1 AND cw.id=wr.withdrawal_id
		  AND cw.status IN ($5,$6) AND cw.posted=FALSE
	`,
		requestID,
		statusFailedReason(fmt.Errorf("%s", reason)),
		crypto_withdrawal.WithdrawalStatusFailed,
		outbound_settlement.StatusNotApplicable,
		crypto_withdrawal.WithdrawalStatusCreated,
		crypto_withdrawal.WithdrawalStatusSigned,
	)
	if err != nil {
		return fmt.Errorf("mark withdrawal execution failed: %w", err)
	}
	return nil
}

func (r *OutboundRepo) FindHotWallet(ctx context.Context, merchantID, networkID, tokenID id.ID, required types.CryptoAmount) (HotWallet, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	var walletRow HotWallet
	err := querier.QueryRow(ctx, `
		SELECT w.id, w.address, w.vault_key_id, COALESCE(b.amount, 0) AS balance
		FROM cat_wallets w
		LEFT JOIN reg_crypto_balance_balances b
		       ON b.wallet_id = w.id AND b.token_id = $2
		WHERE w.network_id = $1
		  AND w.tier = $3
		  AND w.wallet_role = 'liquidity_hot'
		  AND (w.custody_merchant_id=$5 OR (w.custody_merchant_id IS NULL AND NOT EXISTS(
		      SELECT 1 FROM sys_crypto_custody_policies cp WHERE cp.network_id=w.network_id AND cp.enforced)))
		  AND w.is_active = TRUE
		  AND w.deletion_mark = FALSE
		  AND ($4::numeric = 0 OR COALESCE(b.amount, 0) >= $4)
		ORDER BY COALESCE(b.amount, 0) DESC, w.code ASC
		LIMIT 1
	`, networkID, tokenID, wallet.WalletTierHot, required, merchantID).Scan(
		&walletRow.ID,
		&walletRow.Address,
		&walletRow.VaultKeyID,
		&walletRow.Balance,
	)
	if err != nil {
		return HotWallet{}, fmt.Errorf("find hot wallet: %w", err)
	}
	return walletRow, nil
}

// ReserveHotWalletForWithdrawal selects the highest-balance hot wallet that has
// enough unreserved token balance and creates an active reservation for the request.
func (r *OutboundRepo) ReserveHotWalletForWithdrawal(
	ctx context.Context,
	requestID, merchantID, networkID, tokenID id.ID,
	required types.CryptoAmount,
) (HotWallet, error) {
	txm := postgres.MustGetTxManager(ctx)

	var selected HotWallet
	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		querier := txm.GetQuerier(txCtx)
		basis, err := prepareWithdrawalResources(txCtx, requestID)
		if err != nil {
			return err
		}
		if basis.merchant != merchantID || basis.token != tokenID || basis.network != networkID || !basis.amount.Equal(required) {
			return holdExecution(fmt.Errorf("%w: withdrawal request identity mismatch", ErrMerchantCustody))
		}
		enforced, err := postgres.MerchantCustodyEnforced(txCtx, networkID)
		if err != nil {
			return err
		}
		lockedWallet, err := r.selectWithdrawalWallet(txCtx, basis, enforced)
		if err != nil {
			return err
		}
		if err := basis.lockRequest(txCtx); err != nil {
			return err
		}
		var actualMerchant, actualToken, actualNetwork id.ID
		var actualAmount types.CryptoAmount
		if err := querier.QueryRow(txCtx, `SELECT wr.merchant_id,wr.token_id,t.network_id,wr.amount
			FROM doc_withdrawal_requests wr JOIN cat_tokens t ON t.id=wr.token_id WHERE wr.id=$1 FOR UPDATE OF wr`, requestID).
			Scan(&actualMerchant, &actualToken, &actualNetwork, &actualAmount); err != nil {
			return fmt.Errorf("lock withdrawal custody identity: %w", err)
		}
		if actualMerchant != merchantID || actualToken != tokenID || actualNetwork != networkID || !actualAmount.Equal(required) {
			return holdExecution(fmt.Errorf("%w: withdrawal request identity mismatch", ErrMerchantCustody))
		}
		var reservedOwner *id.ID
		var reservedNetwork, reservedToken id.ID
		var reservedAmount types.CryptoAmount

		err = querier.QueryRow(txCtx, `
			SELECT w.id, w.address, w.vault_key_id, COALESCE(b.amount, 0) AS balance,
			       w.custody_merchant_id,w.network_id,r.token_id,r.amount
			FROM reg_crypto_balance_reservations r
			JOIN cat_wallets w ON w.id = r.wallet_id
			LEFT JOIN reg_crypto_balance_balances b
			       ON b.wallet_id = w.id AND b.token_id = r.token_id
			WHERE r.document_type = $2
			  AND r.document_id = $1
			  AND r.status = 'active'
			LIMIT 1
		`, requestID, withdrawal_request.DocumentType).Scan(
			&selected.ID,
			&selected.Address,
			&selected.VaultKeyID,
			&selected.Balance,
			&reservedOwner, &reservedNetwork, &reservedToken, &reservedAmount,
		)
		if err == nil {
			if selected.ID != lockedWallet {
				return holdExecution(fmt.Errorf("%w: withdrawal reservation source changed", ErrMerchantCustody))
			}
			if !ownerMatchesMerchant(reservedOwner, merchantID, enforced) || reservedNetwork != networkID || reservedToken != tokenID || !reservedAmount.Equal(required) {
				return holdExecution(fmt.Errorf("%w: existing reservation has a different custody scope", ErrMerchantCustody))
			}
			if selected.VaultKeyID == "" {
				return fmt.Errorf("reserved hot wallet has empty vault_key_id")
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("find existing withdrawal reservation: %w", err)
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
				WHERE w.network_id = $1
				  AND w.id = $9
				  AND w.tier = $3
				  AND w.wallet_role = 'liquidity_hot'
					  AND (w.custody_merchant_id=$7 OR (NOT $8 AND w.custody_merchant_id IS NULL))
				  AND w.is_active = TRUE
				  AND w.deletion_mark = FALSE
				  AND w.vault_key_id <> ''
				  AND b.amount - COALESCE(r.amount, 0) >= $4
				ORDER BY b.amount - COALESCE(r.amount, 0) DESC, w.code ASC
				FOR UPDATE OF b SKIP LOCKED
				LIMIT 1
			),
			inserted AS (
				INSERT INTO reg_crypto_balance_reservations (
					wallet_id, token_id, amount, purpose, document_type, document_id
				)
				SELECT id, $2, $4, 'withdrawal', $6, $5
				FROM candidate
				RETURNING wallet_id
			)
			SELECT c.id, c.address, c.vault_key_id, c.amount
			FROM candidate c
			JOIN inserted i ON i.wallet_id = c.id
		`, networkID, tokenID, wallet.WalletTierHot, required, requestID, withdrawal_request.DocumentType, merchantID, enforced, lockedWallet).Scan(
			&selected.ID,
			&selected.Address,
			&selected.VaultKeyID,
			&selected.Balance,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoHotWalletLiquidity
		}
		if err != nil {
			return fmt.Errorf("reserve hot wallet: %w", err)
		}
		return nil
	})
	if err != nil {
		return HotWallet{}, err
	}

	return selected, nil
}

// GetWithdrawalSigningAttempt loads the existing immutable signing attempt for
// a withdrawal request. It is used on worker retries after reservation,
// authorization, Vault signing, or uncertain broadcast.
func (r *OutboundRepo) GetWithdrawalSigningAttempt(
	ctx context.Context,
	requestID id.ID,
) (HotWallet, SigningAuthorizationRecord, bool, error) {
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
		 AND (rsv.status IN ('active','consumed') OR (rsv.status='released' AND EXISTS(
		     SELECT 1 FROM doc_crypto_withdrawals execution WHERE execution.basis_type=a.basis_type AND execution.basis_id=a.basis_id
		       AND execution.settlement_status='recorded')))
		JOIN cat_wallets w
		  ON w.id = rsv.wallet_id
		 AND w.address = a.source_address
		 AND w.vault_key_id = a.vault_key_id
		LEFT JOIN doc_withdrawal_requests wr ON wr.id = a.basis_id
		LEFT JOIN reg_crypto_balance_balances b
		  ON b.wallet_id = w.id AND b.token_id = wr.token_id
		WHERE a.basis_type = $2
		  AND a.basis_id = $1
		  AND a.attempt = 1
		ORDER BY a.created_at DESC
		LIMIT 1
	`, requestID, withdrawal_request.DocumentType), &selected, &record)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return HotWallet{}, SigningAuthorizationRecord{}, false, nil
		}
		return HotWallet{}, SigningAuthorizationRecord{}, false, err
	}

	return selected, record, true, nil
}

// ConsumeWithdrawalReservation marks the active request reservation as consumed.
func (r *OutboundRepo) ConsumeWithdrawalReservation(ctx context.Context, requestID id.ID) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	tag, err := querier.Exec(ctx, `
		UPDATE reg_crypto_balance_reservations
		SET status = 'consumed',
		    updated_at = NOW()
		WHERE document_type = $2
		  AND document_id = $1
		  AND status = 'active'
	`, requestID, withdrawal_request.DocumentType)
	if err != nil {
		return fmt.Errorf("consume withdrawal reservation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("active withdrawal reservation not found")
	}
	return nil
}

// ReleaseWithdrawalReservation releases an active request reservation.
func (r *OutboundRepo) ReleaseWithdrawalReservation(ctx context.Context, requestID id.ID) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	_, err := querier.Exec(ctx, `
		UPDATE reg_crypto_balance_reservations
		SET status = 'released',
		    updated_at = NOW()
		WHERE document_type = $2
		  AND document_id = $1
		  AND status = 'active'
	`, requestID, withdrawal_request.DocumentType)
	if err != nil {
		return fmt.Errorf("release withdrawal reservation: %w", err)
	}
	return nil
}

func (r *OutboundRepo) LinkWithdrawalBroadcast(ctx context.Context, requestID, withdrawalID id.ID) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	tag, err := querier.Exec(ctx, `
		UPDATE doc_withdrawal_requests
		SET status = 'broadcast',
		    withdrawal_id = $2,
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = $1
		  AND status IN ('signing','broadcast','held')
		  AND withdrawal_id = $2
		  AND _deleted_at IS NULL
	`, requestID, withdrawalID)
	if err != nil {
		return fmt.Errorf("link withdrawal broadcast: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("withdrawal request was not linked to this execution document")
	}
	return nil
}

// MarkWithdrawalFailed marks the request failed after business-level storno.
func (r *OutboundRepo) MarkWithdrawalFailed(ctx context.Context, requestID id.ID, reason string) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	tag, err := querier.Exec(ctx, `
		UPDATE doc_withdrawal_requests
		SET status = 'failed',
		    rejection_reason = $2,
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = $1
		  AND status IN ('approved','signing')
		  AND _deleted_at IS NULL
	`, requestID, reason)
	if err != nil {
		return fmt.Errorf("mark withdrawal failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("withdrawal request not found or already processed")
	}
	return nil
}

// RequeueWithdrawalRequest moves a claimed request back to approved when execution
// cannot proceed without changing merchant balance accounting.
func (r *OutboundRepo) RequeueWithdrawalRequest(ctx context.Context, requestID id.ID, reason string) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	tag, err := querier.Exec(ctx, `
		UPDATE doc_withdrawal_requests
		SET status = 'approved',
		    rejection_reason = $2,
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = $1
		  AND status = 'signing'
		  AND withdrawal_id IS NULL
		  AND _deleted_at IS NULL
	`, requestID, reason)
	if err != nil {
		return fmt.Errorf("requeue withdrawal request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("withdrawal request cannot be requeued")
	}
	return nil
}

// MarkWithdrawalHeld stops a claimed request for manual review without
// restoring merchant balance. It is used for missing or invalid approval
// artifacts, where automatic storno would hide a potential control-plane issue.
func (r *OutboundRepo) MarkWithdrawalHeld(ctx context.Context, requestID id.ID, reason string) error {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	tag, err := querier.Exec(ctx, `
		UPDATE doc_withdrawal_requests
		SET status = 'held',
		    rejection_reason = $2,
		    updated_at = NOW(),
		    version = version + 1
		WHERE id = $1
		  AND status IN ('approved','signing')
		  AND _deleted_at IS NULL
	`, requestID, reason)
	if err != nil {
		return fmt.Errorf("mark withdrawal held: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("withdrawal request cannot be marked held")
	}
	return nil
}

// ListBroadcastWithdrawals returns request/withdrawal pairs awaiting confirmation.
func (r *OutboundRepo) ListBroadcastWithdrawals(ctx context.Context, limit int) ([]BroadcastWithdrawal, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}

	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, `
		SELECT wr.id,
		       cw.id,
		       t.network_id,
		       cw.tx_hash,
		       COALESCE(n.confirmations_needed, 1) AS required_confs
		FROM doc_withdrawal_requests wr
		JOIN doc_crypto_withdrawals cw ON cw.id = wr.withdrawal_id
		JOIN cat_tokens t ON t.id = wr.token_id
		JOIN cat_blockchain_networks n ON n.id = t.network_id
		WHERE wr.status = 'broadcast'
		  AND cw.status = 'broadcast'
		  AND wr._deleted_at IS NULL
		  AND cw.deletion_mark = FALSE
		  AND cw.tx_hash <> ''
		ORDER BY wr.updated_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list broadcast withdrawals: %w", err)
	}
	defer rows.Close()

	items := make([]BroadcastWithdrawal, 0, limit)
	for rows.Next() {
		var item BroadcastWithdrawal
		if err := rows.Scan(&item.RequestID, &item.WithdrawalID, &item.NetworkID, &item.TxHash, &item.RequiredConfs); err != nil {
			return nil, fmt.Errorf("scan broadcast withdrawal: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// MarkWithdrawalConfirmed finalizes request and withdrawal documents after confirmations.
func (r *OutboundRepo) MarkWithdrawalConfirmed(ctx context.Context, requestID, withdrawalID id.ID) error {
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		querier := txm.GetQuerier(txCtx)
		if _, err := querier.Exec(txCtx, `
			UPDATE doc_crypto_withdrawals
			SET status = 'confirmed',
			    updated_at = NOW(),
			    version = version + 1
			WHERE id = $1
			  AND status = 'broadcast'
			  AND deletion_mark = FALSE
		`, withdrawalID); err != nil {
			return fmt.Errorf("confirm crypto withdrawal: %w", err)
		}

		if _, err := querier.Exec(txCtx, `
			UPDATE doc_withdrawal_requests
			SET status = 'confirmed',
			    updated_at = NOW(),
			    version = version + 1
			WHERE id = $1
			  AND status = 'broadcast'
			  AND _deleted_at IS NULL
		`, requestID); err != nil {
			return fmt.Errorf("confirm withdrawal request: %w", err)
		}
		return nil
	})
}
