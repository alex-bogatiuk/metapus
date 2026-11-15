package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/treasury_withdrawal"
	"metapus/internal/infrastructure/storage/postgres"
)

type TreasuryWorkflowRepo struct{}

func NewTreasuryWorkflowRepo() *TreasuryWorkflowRepo { return &TreasuryWorkflowRepo{} }

func treasuryQuerier(ctx context.Context) postgres.Querier {
	return postgres.MustGetTxManager(ctx).GetQuerier(ctx)
}

func (r *TreasuryWorkflowRepo) UpsertTreasuryAddress(ctx context.Context, networkID id.ID, address, addressKey, label string, actorID id.ID) (id.ID, error) {
	var result id.ID
	var actor any
	if !id.IsNil(actorID) {
		actor = actorID
	}
	err := treasuryQuerier(ctx).QueryRow(ctx, `
		INSERT INTO reg_treasury_withdrawal_addresses (
			network_id, address, address_key, label, created_by, updated_by
		)
		SELECT $1, $2, $3, $4, $5, $5
		WHERE NOT EXISTS (
			SELECT 1 FROM cat_wallets
			WHERE network_id = $1 AND deletion_mark = FALSE AND address_key = $3
		)
		ON CONFLICT (network_id, address_key) DO UPDATE
		SET label = EXCLUDED.label, is_active = TRUE,
		    updated_by = EXCLUDED.updated_by, updated_at = NOW(), _deleted_at = NULL
		RETURNING id
	`, networkID, address, addressKey, label, actor).Scan(&result)
	if errors.Is(err, pgx.ErrNoRows) {
		return id.Nil(), apperror.NewValidation("treasury destination must be external to managed custody").WithDetail("field", "address")
	}
	if err != nil {
		return id.Nil(), fmt.Errorf("upsert treasury address: %w", err)
	}
	return result, nil
}

func (r *TreasuryWorkflowRepo) ListTreasuryAddresses(ctx context.Context) ([]cryptoapp.TreasuryAddress, error) {
	rows, err := treasuryQuerier(ctx).Query(ctx, `
		SELECT id, network_id, address, label, is_active
		FROM reg_treasury_withdrawal_addresses
		WHERE _deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 500
	`)
	if err != nil {
		return nil, fmt.Errorf("list treasury addresses: %w", err)
	}
	defer rows.Close()
	items := make([]cryptoapp.TreasuryAddress, 0, 32)
	for rows.Next() {
		var item cryptoapp.TreasuryAddress
		if err := rows.Scan(&item.ID, &item.NetworkID, &item.Address, &item.Label, &item.IsActive); err != nil {
			return nil, fmt.Errorf("scan treasury address: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TreasuryWorkflowRepo) GetTreasuryWithdrawalNetwork(ctx context.Context, tokenID id.ID) (id.ID, error) {
	var networkID id.ID
	err := treasuryQuerier(ctx).QueryRow(ctx, `
		SELECT network_id FROM cat_tokens WHERE id=$1 AND deletion_mark=FALSE
	`, tokenID).Scan(&networkID)
	return networkID, err
}

func (r *TreasuryWorkflowRepo) ValidateTreasuryWithdrawalContext(ctx context.Context, doc *treasury_withdrawal.TreasuryWithdrawal, addressKey string) error {
	var valid bool
	err := treasuryQuerier(ctx).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM cat_tokens t
			JOIN cat_wallets w ON w.id = $2
			WHERE t.id = $1 AND t.deletion_mark = FALSE
			  AND w.network_id = t.network_id
			  -- Revenue goes to an allowlisted treasury address; a refund goes to
			  -- the destination recorded for it, checked when it is inserted.
			  AND ($5 = 'receipt_refund' OR EXISTS (
			      SELECT 1 FROM reg_treasury_withdrawal_addresses a
			      WHERE a.id = $3 AND a.network_id = t.network_id AND a.address_key = $4
			        AND a.is_active = TRUE AND a._deleted_at IS NULL))
			  AND ($5 <> 'receipt_refund' OR EXISTS (
			      SELECT 1 FROM doc_crypto_custody_receipts c WHERE c.id = $6 AND c.token_id = t.id))
			  AND w.custody_merchant_id IS NULL
                  AND (w.wallet_role='treasury' OR (w.wallet_role='liquidity_hot' AND NOT EXISTS(
                      SELECT 1 FROM sys_crypto_custody_policies cp WHERE cp.network_id=w.network_id AND cp.enforced)))
              AND w.tier = 'hot' AND w.is_active = TRUE
			  AND w.deletion_mark = FALSE AND w.vault_key_id <> ''
			  AND NOT EXISTS (
			      SELECT 1 FROM cat_wallets managed
			      WHERE managed.network_id = t.network_id
			        AND managed.deletion_mark = FALSE
			        AND managed.address_key = $4
			  )
		)
	`, doc.TokenID, doc.SourceWalletID, doc.AddressID, addressKey, string(doc.Purpose), doc.ReceiptID).Scan(&valid)
	if err != nil {
		return fmt.Errorf("validate treasury withdrawal context: %w", err)
	}
	if !valid {
		if doc.IsReceiptRefund() {
			return apperror.NewValidation("refund source wallet must be a platform hot wallet holding the receipt token, and the destination external to custody")
		}
		return apperror.NewValidation("treasury withdrawal source wallet or destination allowlist is invalid")
	}
	return nil
}

// GetRefundableReceipt reads what is left of an unidentified receipt to return.
// An orphaned receipt cannot be refunded: its transfer is no longer on chain.
func (r *TreasuryWorkflowRepo) GetRefundableReceipt(ctx context.Context, receiptID id.ID) (cryptoapp.RefundableReceipt, error) {
	var result cryptoapp.RefundableReceipt
	err := treasuryQuerier(ctx).QueryRow(ctx, `
		SELECT c.id, c.number, c.network_id, c.token_id, COALESCE(s.amount, 0),
		       COALESCE(a.status = 'orphaned', FALSE)
		FROM doc_crypto_custody_receipts c
		LEFT JOIN reg_crypto_receipt_suspense_balances s ON s.receipt_id = c.id AND s.token_id = c.token_id
		LEFT JOIN reg_crypto_custody_receipt_audits a ON a.receipt_id = c.id
		WHERE c.id = $1
	`, receiptID).Scan(&result.ReceiptID, &result.Number, &result.NetworkID, &result.TokenID, &result.Remainder, &result.Orphaned)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, apperror.NewNotFound("crypto custody receipt", receiptID)
	}
	if err != nil {
		return result, fmt.Errorf("read refundable receipt: %w", err)
	}
	return result, nil
}

// LockReceiptRemainder locks the receipt remainder as every manual
// attribution does, so a refund and a credit or funding serialize.
func (r *TreasuryWorkflowRepo) LockReceiptRemainder(ctx context.Context, receiptID id.ID) (cryptoapp.ReceiptRemainder, error) {
	return lockReceiptRemainder(ctx, receiptID)
}

// CreateReceiptRefundDestination records where a refund pays and why. The
// database refuses an address of managed custody or of another network.
func (r *TreasuryWorkflowRepo) CreateReceiptRefundDestination(ctx context.Context, d cryptoapp.ReceiptRefundDestination) error {
	_, err := treasuryQuerier(ctx).Exec(ctx, `
		INSERT INTO reg_crypto_receipt_refund_destinations(id, receipt_id, network_id, address, address_key, evidence, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, d.ID, d.ReceiptID, d.NetworkID, d.Address, d.AddressKey, d.Evidence, d.CreatedBy)
	if err != nil {
		return fmt.Errorf("record receipt refund destination: %w", err)
	}
	return nil
}

func (r *TreasuryWorkflowRepo) GetTreasuryAvailability(ctx context.Context, tokenID id.ID) (cryptoapp.TreasuryAvailability, error) {
	result := cryptoapp.TreasuryAvailability{TokenID: tokenID}
	err := treasuryQuerier(ctx).QueryRow(ctx, `
		WITH active_reserved AS (
			SELECT token_id, SUM(amount) AS amount
			FROM reg_crypto_revenue_reservations
			WHERE token_id = $1 AND status = 'active'
			GROUP BY token_id
		)
		SELECT COALESCE(SUM(b.balance), 0), COALESCE(MAX(r.amount), 0)
		FROM reg_crypto_fee_balances b
		LEFT JOIN active_reserved r ON r.token_id = b.token_id
		WHERE b.token_id = $1
	`, tokenID).Scan(&result.Amount, &result.Reserved)
	if err != nil {
		return result, fmt.Errorf("get treasury availability: %w", err)
	}
	result.Available = types.ZeroCryptoAmount()
	if result.Amount.Cmp(result.Reserved) >= 0 {
		result.Available = result.Amount.Sub(result.Reserved)
	}
	return result, nil
}

func (r *TreasuryWorkflowRepo) ListTreasurySourceWallets(ctx context.Context, tokenID id.ID) ([]cryptoapp.TreasurySourceWallet, error) {
	rows, err := treasuryQuerier(ctx).Query(ctx, `
		WITH active_reserved AS (
			SELECT wallet_id, token_id, SUM(amount) AS amount
			FROM reg_crypto_balance_reservations
			WHERE token_id = $1 AND status = 'active'
			GROUP BY wallet_id, token_id
		), latest_recon AS (
			SELECT DISTINCT ON (wallet_id, token_id) wallet_id, token_id, chain_amount, last_seen_at
			FROM reg_crypto_reconciliation_items
			WHERE token_id = $1 AND status IN ('open','acknowledged')
			ORDER BY wallet_id, token_id, last_seen_at DESC
		)
		SELECT w.id, w.network_id, w.name, w.address,
		       COALESCE(b.amount, 0), COALESCE(r.amount, 0),
		       COALESCE(b.amount, 0) - COALESCE(r.amount, 0),
		       lr.chain_amount, lr.last_seen_at
		FROM cat_wallets w
		JOIN cat_tokens t ON t.id = $1 AND t.network_id = w.network_id
		LEFT JOIN reg_crypto_balance_balances b ON b.wallet_id = w.id AND b.token_id = t.id
		LEFT JOIN active_reserved r ON r.wallet_id = w.id AND r.token_id = t.id
		LEFT JOIN latest_recon lr ON lr.wallet_id = w.id AND lr.token_id = t.id
		WHERE w.custody_merchant_id IS NULL
                  AND (w.wallet_role='treasury' OR (w.wallet_role='liquidity_hot' AND NOT EXISTS(
                      SELECT 1 FROM sys_crypto_custody_policies cp WHERE cp.network_id=w.network_id AND cp.enforced)))
          AND w.tier = 'hot' AND w.is_active = TRUE AND w.deletion_mark = FALSE
		  AND w.vault_key_id <> ''
		ORDER BY w.name, w.id
		LIMIT 100
	`, tokenID)
	if err != nil {
		return nil, fmt.Errorf("list treasury source wallets: %w", err)
	}
	defer rows.Close()
	items := make([]cryptoapp.TreasurySourceWallet, 0, 8)
	for rows.Next() {
		var item cryptoapp.TreasurySourceWallet
		var chainAmount postgres.NullableCryptoAmount
		if err := rows.Scan(&item.ID, &item.NetworkID, &item.Name, &item.Address,
			&item.LedgerAmount, &item.ReservedAmount, &item.AvailableAmount,
			&chainAmount, &item.LastReconciliationAt); err != nil {
			return nil, fmt.Errorf("scan treasury source wallet: %w", err)
		}
		if chainAmount.Valid {
			value := chainAmount.Amount.String()
			item.LastChainAmount = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TreasuryWorkflowRepo) LockTreasuryWithdrawal(ctx context.Context, withdrawalID id.ID) (cryptoapp.LockedTreasuryWithdrawal, error) {
	var result cryptoapp.LockedTreasuryWithdrawal
	err := treasuryQuerier(ctx).QueryRow(ctx, `
		SELECT status, created_by, token_id, amount
		FROM doc_treasury_withdrawals
		WHERE id = $1 AND _deleted_at IS NULL
		FOR UPDATE
	`, withdrawalID).Scan(&result.Status, &result.CreatedBy, &result.TokenID, &result.Amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, apperror.NewNotFound("treasury withdrawal", withdrawalID)
	}
	if err != nil {
		return result, fmt.Errorf("lock treasury withdrawal: %w", err)
	}
	return result, nil
}

func (r *TreasuryWorkflowRepo) MarkTreasuryWithdrawalApproved(ctx context.Context, withdrawalID, actorID id.ID) error {
	tag, err := treasuryQuerier(ctx).Exec(ctx, `
		UPDATE doc_treasury_withdrawals
		SET status='approved', approved_by=$2, approved_at=NOW(),
		    updated_by=$2, updated_at=NOW(), version=version+1
		WHERE id=$1 AND status='pending_approval' AND _deleted_at IS NULL
	`, withdrawalID, actorID)
	if err != nil {
		return fmt.Errorf("approve treasury withdrawal: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apperror.NewValidation("treasury withdrawal cannot be approved")
	}
	return nil
}

func (r *TreasuryWorkflowRepo) MarkTreasuryWithdrawalRejected(ctx context.Context, withdrawalID, actorID id.ID, reason string) error {
	tag, err := treasuryQuerier(ctx).Exec(ctx, `
		UPDATE doc_treasury_withdrawals
		SET status='rejected', rejection_reason=$2, updated_by=$3,
		    updated_at=NOW(), version=version+1
		WHERE id=$1 AND status IN ('pending_signature','pending_approval','approved') AND _deleted_at IS NULL
	`, withdrawalID, reason, actorID)
	if err != nil {
		return fmt.Errorf("reject treasury withdrawal: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apperror.NewValidation("treasury withdrawal cannot be rejected")
	}
	return nil
}

var _ cryptoapp.TreasuryWorkflowRepository = (*TreasuryWorkflowRepo)(nil)
