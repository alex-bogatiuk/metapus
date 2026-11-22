package crypto_repo

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

const amlWithdrawalRequestResourcesSQL = `WITH lines AS MATERIALIZED (
 SELECT id,wallet_id,withdrawal_id FROM reg_crypto_aml_withdrawal_lines WHERE request_id=$1
), executions AS (
 SELECT withdrawal_id id FROM lines WHERE withdrawal_id IS NOT NULL
 UNION SELECT withdrawal_id FROM doc_withdrawal_requests WHERE id=$1 AND withdrawal_id IS NOT NULL
), wallets AS (
 SELECT wallet_id id FROM lines
 UNION SELECT wallet_id FROM reg_crypto_balance_reservations WHERE document_type='WithdrawalRequest' AND document_id=$1
 UNION SELECT source_wallet_id FROM doc_crypto_withdrawals WHERE id IN (SELECT id FROM executions)
 UNION SELECT payer_wallet_id FROM reg_crypto_fee_reservations
 WHERE (basis_type='WithdrawalRequest' AND basis_id=$1) OR (basis_type='CryptoAMLWithdrawalLine' AND basis_id IN (SELECT id FROM lines))
 UNION SELECT source_wallet_id FROM doc_crypto_native_fundings WHERE status NOT IN ('settled','failed')
 AND ((basis_type='WithdrawalRequest' AND basis_id=$1) OR (basis_type='CryptoAMLWithdrawalLine' AND basis_id IN (SELECT id FROM lines)))
 UNION SELECT destination_wallet_id FROM doc_crypto_native_fundings WHERE status NOT IN ('settled','failed')
 AND ((basis_type='WithdrawalRequest' AND basis_id=$1) OR (basis_type='CryptoAMLWithdrawalLine' AND basis_id IN (SELECT id FROM lines)))
)
 SELECT ARRAY(SELECT id FROM executions ORDER BY id),ARRAY(SELECT id FROM wallets ORDER BY id)`

// Approval/rejection callers take this prefix before the parent request row.
// Rechecking the planned footprint prevents discovering a new execution/FK
// wallet only after a concurrent scope owner has committed.
func (r *AMLRepository) LockAMLWithdrawalRequest(ctx context.Context, requestID id.ID) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("AML withdrawal resources require a transaction")
	}
	q := tm.GetQuerier(ctx)
	var merchant, network id.ID
	err := q.QueryRow(ctx, `SELECT merchant_id,network_id FROM reg_crypto_aml_withdrawal_plans WHERE request_id=$1`, requestID).Scan(&merchant, &network)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var isolation string
	if err := q.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
		return err
	}
	if isolation != "read committed" {
		return apperror.NewValidation("AML withdrawal resources require read committed isolation")
	}
	locker := postgres.NewDocLocker()
	if err := locker.LockDocument(ctx, "WithdrawalRequest", requestID); err != nil {
		return err
	}
	var executions, wallets []id.ID
	if err := q.QueryRow(ctx, amlWithdrawalRequestResourcesSQL, requestID).Scan(&executions, &wallets); err != nil {
		return err
	}
	for _, execution := range executions {
		if err := locker.LockDocument(ctx, "CryptoWithdrawal", execution); err != nil {
			return err
		}
	}
	if _, err := q.Exec(ctx, `SELECT fn_crypto_lock_withdrawal_wallets($1::uuid[],$2,$3)`, wallets, merchant, network); err != nil {
		return err
	}
	var currentExecutions, currentWallets []id.ID
	if err := q.QueryRow(ctx, amlWithdrawalRequestResourcesSQL, requestID).Scan(&currentExecutions, &currentWallets); err != nil {
		return err
	}
	if !slices.Equal(executions, currentExecutions) || !slices.Equal(wallets, currentWallets) {
		return apperror.NewConflict("withdrawal resource footprint changed; retry the command")
	}
	return nil
}
