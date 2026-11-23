package outbound_repo

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

func (r *OutboundRepo) LockWithdrawalCompletion(ctx context.Context, task WithdrawalTask) (outboundapp.WithdrawalCompletionState, error) {
	var state outboundapp.WithdrawalCompletionState
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if id.IsNil(task.AMLParentID) {
		basis, err := prepareWithdrawalResources(ctx, task.RequestID)
		if err != nil {
			return state, err
		}
		if _, err := basis.lockWallets(ctx, nil, false); err != nil {
			return state, err
		}
		if err := basis.lockRequest(ctx); err != nil {
			return state, err
		}
		err = q.QueryRow(ctx, `SELECT withdrawal_id,status FROM doc_withdrawal_requests WHERE id=$1`, task.RequestID).Scan(&state.DocumentID, &state.Status)
		return state, err
	}
	var execution *id.ID
	query := `SELECT c.id FROM doc_withdrawal_requests r JOIN doc_crypto_withdrawals c ON c.id=r.withdrawal_id WHERE r.id=$1`
	if !id.IsNil(task.AMLParentID) {
		query = `SELECT c.id FROM reg_crypto_aml_withdrawal_lines l JOIN doc_crypto_withdrawals c ON c.id=l.withdrawal_id WHERE l.id=$1`
	}
	// No execution yet is an ordinary pre-effect state; the locked read below
	// returns the authoritative request/line state.
	if err := q.QueryRow(ctx, query, task.RequestID).Scan(&execution); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return state, err
	}
	if execution != nil {
		if err := postgres.NewDocLocker().LockDocument(ctx, "CryptoWithdrawal", *execution); err != nil {
			return state, err
		}
	}
	if _, err := crypto_repo.NewAMLRepository().LockAMLWithdrawalScope(ctx, task.MerchantID, task.NetworkID, task.TokenID); err != nil {
		return state, err
	}
	if _, err := q.Exec(ctx, `SELECT fn_crypto_lock_custody_wallets(ARRAY(
		SELECT wallet_id FROM reg_crypto_aml_withdrawal_lines WHERE id=$1 AND request_id=$2
		UNION SELECT payer_wallet_id FROM reg_crypto_fee_reservations WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=$1 AND status IN ('reserved','broadcast','held')
		UNION SELECT source_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=$1 AND status NOT IN ('settled','failed')
		UNION SELECT destination_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=$1 AND status NOT IN ('settled','failed')))`, task.RequestID, task.AMLParentID); err != nil {
		return state, err
	}
	err := q.QueryRow(ctx, `SELECT withdrawal_id,status FROM reg_crypto_aml_withdrawal_lines WHERE id=$1 AND request_id=$2 FOR UPDATE`, task.RequestID, task.AMLParentID).Scan(&state.DocumentID, &state.Status)
	if err == nil && ((execution == nil) != (state.DocumentID == nil) || (execution != nil && *execution != *state.DocumentID)) {
		return state, apperror.NewConflict("AML withdrawal execution identity changed")
	}
	return state, err
}

func (r *OutboundRepo) LinkAMLWithdrawalBroadcast(ctx context.Context, task WithdrawalTask, authorizationID, txHash string) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `UPDATE reg_crypto_aml_withdrawal_lines SET status='broadcast',lease_token=NULL,lease_expires_at=NULL,next_attempt_at=NOW()+interval '30 seconds',error_code='' WHERE id=$1`, task.RequestID); err != nil {
		return err
	}
	if err := r.MarkSigningAuthorizationBroadcast(ctx, authorizationID, txHash); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE doc_withdrawal_requests SET status=CASE WHEN status='held' THEN status ELSE 'broadcast' END,version=version+1,updated_at=NOW() WHERE id=$1`, task.AMLParentID)
	return err
}
