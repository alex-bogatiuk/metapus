package crypto_repo

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

func (r *AMLRepository) ClaimAMLWithdrawalLines(ctx context.Context, limit int) ([]crypto.WithdrawalExecutionTask, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	tm := postgres.MustGetTxManager(ctx)
	var tasks []crypto.WithdrawalExecutionTask
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var merchant, network, token id.ID
		err := q.QueryRow(ctx, `SELECT p.merchant_id,p.network_id,p.token_id FROM reg_crypto_aml_withdrawal_lines l JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id JOIN doc_withdrawal_requests r ON r.id=p.request_id
 JOIN sys_crypto_custody_policies custody ON custody.network_id=p.network_id AND custody.enforced
 WHERE l.status IN ('reserved','held','broadcast') AND l.next_attempt_at<=NOW() AND (l.lease_expires_at IS NULL OR l.lease_expires_at<=NOW()) AND (r.status IN ('approved','signing','broadcast') OR (r.status='held' AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.basis_type='CryptoAMLWithdrawalLine' AND a.basis_id=l.id)))
 ORDER BY l.next_attempt_at,l.id FOR UPDATE OF custody SKIP LOCKED LIMIT 1`).Scan(&merchant, &network, &token)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := r.LockAMLWithdrawalScope(ctx, merchant, network, token); err != nil {
			return err
		}
		rows, err := q.Query(ctx, `WITH picked AS(
   SELECT l.id FROM reg_crypto_aml_withdrawal_lines l JOIN doc_withdrawal_requests r ON r.id=l.request_id JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id AND p.merchant_id=$2 AND p.network_id=$3
   WHERE l.status IN ('reserved','held','broadcast') AND l.next_attempt_at<=NOW() AND (l.lease_expires_at IS NULL OR l.lease_expires_at<=NOW())
   AND (r.status IN ('approved','signing','broadcast') OR (r.status='held' AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.basis_type='CryptoAMLWithdrawalLine' AND a.basis_id=l.id)))
   ORDER BY l.next_attempt_at,l.id FOR UPDATE OF l SKIP LOCKED LIMIT $1
  ), claimed AS(UPDATE reg_crypto_aml_withdrawal_lines l SET lease_token=gen_random_uuid_v7(),lease_expires_at=NOW()+interval '2 minutes',next_attempt_at=NOW()+interval '30 seconds' FROM picked WHERE l.id=picked.id RETURNING l.*)
  SELECT l.id,l.request_id,l.lease_token,l.wallet_id,l.source_address,l.source_vault_key_id,COALESCE(b.amount,0),r.merchant_id,l.token_id,t.network_id,n.code,l.amount,r.dest_address,COALESCE(t.contract_address,''),t.token_standard,t.decimal_places,n.confirmations_needed,r.posted_version,r.status='held'
  FROM claimed l JOIN doc_withdrawal_requests r ON r.id=l.request_id JOIN cat_tokens t ON t.id=l.token_id JOIN cat_blockchain_networks n ON n.id=t.network_id LEFT JOIN reg_crypto_balance_balances b ON b.wallet_id=l.wallet_id AND b.token_id=l.token_id ORDER BY l.id`, limit, merchant, network)
		if err != nil {
			return err
		}
		for rows.Next() {
			var t crypto.WithdrawalExecutionTask
			if err := rows.Scan(&t.RequestID, &t.AMLParentID, &t.AMLLeaseToken, &t.AMLSource.ID, &t.AMLSource.Address, &t.AMLSource.VaultKeyID, &t.AMLSource.Balance, &t.MerchantID, &t.TokenID, &t.NetworkID, &t.NetworkCode, &t.Amount, &t.DestAddress, &t.ContractAddress, &t.TokenStandard, &t.TokenDecimals, &t.RequiredConfs, &t.PostedVersion, &t.RecoveryOnly); err != nil {
				rows.Close()
				return err
			}
			tasks = append(tasks, t)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if _, err := q.Exec(ctx, `UPDATE doc_withdrawal_requests SET status='signing',version=version+1,updated_at=NOW() WHERE id=$1 AND status='approved'`, task.AMLParentID); err != nil {
				return err
			}
		}
		return nil
	})
	return tasks, err
}
