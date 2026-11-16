package outbound_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/infrastructure/storage/postgres"
)

type withdrawalResourceBasis struct {
	request, merchant, network, token id.ID
	amount                            types.RawAmount
	execution                         *id.ID
}

func prepareWithdrawalResources(ctx context.Context, request id.ID) (withdrawalResourceBasis, error) {
	b := withdrawalResourceBasis{request: request}
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return b, apperror.NewValidation("withdrawal resources require a transaction")
	}
	locker := postgres.NewDocLocker()
	if err := locker.LockDocument(ctx, withdrawal_request.DocumentType, request); err != nil {
		return b, err
	}
	err := tm.GetQuerier(ctx).QueryRow(ctx, `SELECT r.merchant_id,t.network_id,r.token_id,r.amount,r.withdrawal_id
		FROM doc_withdrawal_requests r JOIN cat_tokens t ON t.id=r.token_id WHERE r.id=$1 AND r._deleted_at IS NULL`, request).
		Scan(&b.merchant, &b.network, &b.token, &b.amount, &b.execution)
	if err != nil {
		return b, err
	}
	if b.execution != nil {
		if err := locker.LockDocument(ctx, "CryptoWithdrawal", *b.execution); err != nil {
			return b, err
		}
	}
	return b, nil
}

// Include the executing source, pending fees and autonomous native funding.
// A parent cancellation must not acquire their wallets after the request row.
const withdrawalResourceWalletsSQL = `SELECT wallet_id FROM reg_crypto_balance_reservations WHERE document_type='WithdrawalRequest' AND document_id=$1
 UNION SELECT payer_wallet_id FROM reg_crypto_fee_reservations WHERE basis_type='WithdrawalRequest' AND basis_id=$1 AND status IN ('reserved','broadcast','held')
 UNION SELECT c.source_wallet_id FROM doc_crypto_withdrawals c JOIN doc_withdrawal_requests r ON r.withdrawal_id=c.id WHERE r.id=$1
 UNION SELECT source_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='WithdrawalRequest' AND basis_id=$1 AND status NOT IN ('settled','failed')
 UNION SELECT destination_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='WithdrawalRequest' AND basis_id=$1 AND status NOT IN ('settled','failed')`

func (b withdrawalResourceBasis) lockWallets(ctx context.Context, extra *id.ID, skipBusy bool) (bool, error) {
	var wallets []id.ID
	if extra != nil {
		wallets = append(wallets, *extra)
	}
	return b.lockWalletSet(ctx, wallets, skipBusy)
}

func (b withdrawalResourceBasis) lockWalletSet(ctx context.Context, extra []id.ID, skipBusy bool) (bool, error) {
	function := "fn_crypto_lock_withdrawal_wallets"
	if skipBusy {
		function = "fn_crypto_try_lock_custody_wallets"
	}
	query := `SELECT ` + function + `(ARRAY(` + withdrawalResourceWalletsSQL + ` UNION SELECT unnest($2::uuid[])),$3::uuid,$4::uuid)`
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if skipBusy {
		var acquired bool
		err := q.QueryRow(ctx, query, b.request, extra, b.merchant, b.network).Scan(&acquired)
		return acquired, err
	}
	_, err := q.Exec(ctx, query, b.request, extra, b.merchant, b.network)
	return err == nil, err
}

func (b withdrawalResourceBasis) lockRequest(ctx context.Context) error {
	var current bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT merchant_id=$2 AND token_id=$3 AND amount=$4 AND withdrawal_id IS NOT DISTINCT FROM $5::uuid
		FROM doc_withdrawal_requests WHERE id=$1 AND _deleted_at IS NULL FOR UPDATE`, b.request, b.merchant, b.token, b.amount, b.execution).Scan(&current)
	if err != nil {
		return err
	}
	if !current {
		return apperror.NewConflict("withdrawal resource identity changed")
	}
	return nil
}

func (r *OutboundRepo) selectWithdrawalWallet(ctx context.Context, b withdrawalResourceBasis, enforced bool) (id.ID, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var existing *id.ID
	var valid bool
	err := q.QueryRow(ctx, `SELECT r.wallet_id,r.token_id=$2 AND r.amount=$3 AND w.network_id=$4
		AND (w.custody_merchant_id=$5 OR (NOT $6 AND w.custody_merchant_id IS NULL))
		FROM reg_crypto_balance_reservations r JOIN cat_wallets w ON w.id=r.wallet_id
		WHERE r.document_type='WithdrawalRequest' AND r.document_id=$1 AND r.status='active' LIMIT 1`, b.request, b.token, b.amount, b.network, b.merchant, enforced).Scan(&existing, &valid)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return id.Nil(), err
	}
	if existing != nil {
		if !valid {
			return id.Nil(), holdExecution(fmt.Errorf("%w: existing reservation has a different custody scope", ErrMerchantCustody))
		}
		_, err := b.lockWallets(ctx, existing, false)
		return *existing, err
	}
	rows, err := q.Query(ctx, `SELECT w.id FROM cat_wallets w JOIN reg_crypto_balance_balances balance ON balance.wallet_id=w.id AND balance.token_id=$2
 LEFT JOIN LATERAL (SELECT SUM(amount) amount FROM reg_crypto_balance_reservations WHERE wallet_id=w.id AND token_id=$2 AND status='active') reserved ON TRUE
 WHERE w.network_id=$1 AND w.tier='hot' AND w.wallet_role='liquidity_hot' AND w.is_active AND NOT w.deletion_mark AND w.vault_key_id<>''
 AND (w.custody_merchant_id=$3 OR (NOT $4 AND w.custody_merchant_id IS NULL)) AND balance.amount-COALESCE(reserved.amount,0)>=$5
 ORDER BY balance.amount-COALESCE(reserved.amount,0) DESC,w.code,w.id`, b.network, b.token, b.merchant, enforced, b.amount)
	if err != nil {
		return id.Nil(), err
	}
	var candidates []id.ID
	for rows.Next() {
		var candidate id.ID
		if err := rows.Scan(&candidate); err != nil {
			rows.Close()
			return id.Nil(), err
		}
		candidates = append(candidates, candidate)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return id.Nil(), err
	}
	for _, candidate := range candidates {
		acquired, err := b.lockWallets(ctx, &candidate, true)
		if err != nil {
			return id.Nil(), err
		}
		if acquired {
			return candidate, nil
		}
	}
	return id.Nil(), ErrNoHotWalletLiquidity
}
