package outbound_repo

import (
	"context"
	"errors"
	"fmt"

	"metapus/internal/configuration/crypto/domain/documents/crypto_withdrawal"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

func (r *OutboundRepo) ClaimAMLWithdrawalLines(ctx context.Context, limit int) ([]WithdrawalTask, error) {
	return crypto_repo.NewAMLRepository().ClaimAMLWithdrawalLines(ctx, limit)
}

func (r *OutboundRepo) DeferAMLWithdrawalLine(ctx context.Context, task WithdrawalTask, cause error) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if _, err := crypto_repo.NewAMLRepository().LockAMLWithdrawalScope(ctx, task.MerchantID, task.NetworkID, task.TokenID); err != nil {
			return err
		}
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		state, code := "held", "execution_requires_recovery"
		if errors.Is(cause, ErrNativeFundingPending) {
			state, code = "reserved", "awaiting_native_funding"
		}
		tag, err := q.Exec(ctx, `UPDATE reg_crypto_aml_withdrawal_lines SET status=CASE WHEN status='broadcast' THEN status ELSE $3 END,error_code=$4,lease_token=NULL,lease_expires_at=NULL,next_attempt_at=NOW()+interval '30 seconds' WHERE id=$1 AND lease_token=$2 AND status IN ('reserved','held','broadcast')`, task.RequestID, task.AMLLeaseToken, state, code)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		if state == "held" {
			_, err = q.Exec(ctx, `UPDATE doc_withdrawal_requests SET status='held',rejection_reason='One or more AML source lines require recovery',version=version+1,updated_at=NOW() WHERE id=$1 AND status IN ('approved','signing','broadcast')`, task.AMLParentID)
		}
		return err
	})
}

func (r *OutboundRepo) EnsureAMLWithdrawalExecution(ctx context.Context, task WithdrawalTask, candidate *crypto_withdrawal.CryptoWithdrawal, repo crypto_withdrawal.Repository) (*crypto_withdrawal.CryptoWithdrawal, error) {
	var result *crypto_withdrawal.CryptoWithdrawal
	tm := postgres.MustGetTxManager(ctx)
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		if _, err := crypto_repo.NewAMLRepository().LockAMLWithdrawalScope(ctx, task.MerchantID, task.NetworkID, task.TokenID); err != nil {
			return err
		}
		q := tm.GetQuerier(ctx)
		var existing *id.ID
		if err := q.QueryRow(ctx, `SELECT withdrawal_id FROM reg_crypto_aml_withdrawal_lines WHERE id=$1 AND request_id=$2 AND status IN ('reserved','held') FOR UPDATE`, task.RequestID, task.AMLParentID).Scan(&existing); err != nil {
			return err
		}
		if existing != nil {
			var err error
			result, err = repo.GetByID(ctx, *existing)
			return err
		}
		if candidate == nil || candidate.BasisType != task.SigningBasis() || candidate.BasisID == nil || *candidate.BasisID != task.RequestID || candidate.SourceWalletID != task.AMLSource.ID || !candidate.Amount.Equal(task.Amount) {
			return fmt.Errorf("AML execution candidate differs from source line")
		}
		if err := repo.Create(ctx, candidate); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `UPDATE reg_crypto_aml_withdrawal_lines SET withdrawal_id=$2 WHERE id=$1`, task.RequestID, candidate.ID)
		result = candidate
		return err
	})
	return result, err
}

func recordAMLWithdrawalFact(ctx context.Context, task outboundSettlementTask, succeeded bool) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	_, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_withdrawal_facts(line_id,wallet_id,token_id,amount,succeeded,settlement_id,expense_movement_id)
 SELECT l.id,l.wallet_id,l.token_id,l.amount,$2,s.id,m.line_id
 FROM reg_crypto_aml_withdrawal_lines l JOIN doc_crypto_withdrawals w ON w.id=l.withdrawal_id
 JOIN reg_crypto_chain_settlements s ON s.network_id=$3 AND s.tx_hash=w.tx_hash AND s.basis_type='CryptoAMLWithdrawalLine' AND s.basis_id=l.id
 LEFT JOIN reg_crypto_balance_movements m ON m.recorder_id=w.id AND m.recorder_type='CryptoWithdrawal' AND m.recorder_version=1 AND m.record_type='expense'
 WHERE l.id=$1 ON CONFLICT(line_id) DO NOTHING`, task.LineID, succeeded, task.NetworkID)
	if err != nil {
		return err
	}
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_facts WHERE line_id=$1 AND succeeded=$2)`, task.LineID, succeeded).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("AML withdrawal has no exact final financial fact")
	}
	_, err = q.Exec(ctx, `SELECT fn_crypto_aml_refresh_withdrawal($1)`, task.RequestID)
	return err
}
