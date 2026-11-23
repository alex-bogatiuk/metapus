package crypto_repo

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

// NudgePending asks the existing receipt workers to poll their own accepted
// operation. It never clears a lease/hold, changes a tx, signs or posts money.
// Delivery state is separate from immutable requests: observers never wait on
// a queue row held by a worker that is waiting for financial resources.
func (*ExpectedTransferRepo) NudgePending(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	tm := postgres.MustGetTxManager(ctx)
	rows, err := tm.GetQuerier(ctx).Query(ctx, `SELECT r.expected_effect_id FROM reg_crypto_expected_transfer_requests r
 LEFT JOIN sys_crypto_expected_transfer_deliveries d ON d.expected_effect_id=r.expected_effect_id
 WHERE d.delivered_at IS NULL AND COALESCE(d.next_attempt_at,r.requested_at)<=now()
 ORDER BY COALESCE(d.next_attempt_at,r.requested_at),r.expected_effect_id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	var ids []id.ID
	for rows.Next() {
		var value id.ID
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	delivered := 0
	for _, value := range ids {
		var ready bool
		err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
			q := tm.GetQuerier(ctx)
			var ownerType, hash string
			var owner, network id.ID
			var exact bool
			err := q.QueryRow(ctx, `SELECT r.owner_type,r.owner_id,x.tx_hash,x.network_id,
    (SELECT count(*)=1 AND COALESCE(bool_and(c.intent_valid AND c.expected_id=x.id),false)
      FROM fn_crypto_expected_transfer_candidates(x.network_id,x.tx_hash,x.event_kind,x.source_address_key,x.destination_address_key,x.contract_address_key,x.amount) c)
    FROM reg_crypto_expected_transfer_requests r JOIN reg_crypto_expected_chain_effects x ON x.id=r.expected_effect_id WHERE r.expected_effect_id=$1`, value).Scan(&ownerType, &owner, &hash, &network, &exact)
			if err != nil {
				return err
			}
			if exact {
				var sql string
				switch ownerType {
				case "CryptoNativeFunding":
					if err := LockNativeFundingResources(ctx, owner); err != nil {
						return err
					}
					sql = `UPDATE doc_crypto_native_fundings SET next_attempt_at=LEAST(next_attempt_at,now()) WHERE id=$1 AND network_id=$2 AND regexp_replace(lower(tx_hash),'^0x','')=regexp_replace(lower($3),'^0x','') AND status IN ('broadcast','solidified') RETURNING true`
				case "CryptoSweepAttempt":
					sql = `UPDATE doc_crypto_sweeps__lines l SET settlement_checked_at='epoch' FROM doc_crypto_sweeps d JOIN reg_crypto_sweep_attempts a ON a.sweep_id=d.id WHERE a.id=$1 AND l.line_id=a.id AND l.document_id=d.id AND a.network_id=$2 AND regexp_replace(lower(l.tx_hash),'^0x','')=regexp_replace(lower($3),'^0x','') AND l.settlement_status IN ('unknown','repairing','held') RETURNING true`
				case "TreasuryWithdrawal":
					sql = `UPDATE doc_treasury_withdrawals d SET settlement_checked_at='epoch' FROM cat_tokens t WHERE d.id=$1 AND t.id=d.token_id AND t.network_id=$2 AND regexp_replace(lower(tx_hash),'^0x','')=regexp_replace(lower($3),'^0x','') AND settlement_status IN ('unknown','repairing','held') RETURNING true`
				case "WithdrawalRequest", "CryptoAMLWithdrawalLine":
					sql = `UPDATE doc_crypto_withdrawals d SET settlement_checked_at='epoch' FROM cat_tokens t WHERE d.basis_type=$4 AND d.basis_id=$1 AND t.id=d.token_id AND t.network_id=$2 AND regexp_replace(lower(d.tx_hash),'^0x','')=regexp_replace(lower($3),'^0x','') AND d.settlement_status IN ('unknown','repairing','held') RETURNING true`
				}
				if sql != "" {
					args := []any{owner, network, hash}
					if ownerType == "WithdrawalRequest" || ownerType == "CryptoAMLWithdrawalLine" {
						args = append(args, ownerType)
					}
					tag, err := q.Exec(ctx, sql, args...)
					if err != nil {
						return err
					}
					ready = tag.RowsAffected() > 0
				}
				if !ready {
					// Completed operations acknowledge the handoff without reopening state.
					err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM doc_crypto_native_fundings f WHERE $1='CryptoNativeFunding' AND f.id=$2 AND f.network_id=$3 AND regexp_replace(lower(f.tx_hash),'^0x','')=regexp_replace(lower($4),'^0x','') AND f.status IN ('settled','failed'))
     OR EXISTS(SELECT 1 FROM doc_treasury_withdrawals d JOIN cat_tokens t ON t.id=d.token_id WHERE $1='TreasuryWithdrawal' AND d.id=$2 AND t.network_id=$3 AND regexp_replace(lower(d.tx_hash),'^0x','')=regexp_replace(lower($4),'^0x','') AND d.settlement_status='recorded')
     OR EXISTS(SELECT 1 FROM doc_crypto_withdrawals d JOIN cat_tokens t ON t.id=d.token_id WHERE d.basis_type=$1 AND d.basis_id=$2 AND t.network_id=$3 AND regexp_replace(lower(d.tx_hash),'^0x','')=regexp_replace(lower($4),'^0x','') AND d.settlement_status='recorded')
     OR EXISTS(SELECT 1 FROM doc_crypto_sweeps__lines l JOIN reg_crypto_sweep_attempts a ON a.id=l.line_id WHERE $1='CryptoSweepAttempt' AND a.id=$2 AND a.network_id=$3 AND regexp_replace(lower(l.tx_hash),'^0x','')=regexp_replace(lower($4),'^0x','') AND l.settlement_status='recorded')`, ownerType, owner, network, hash).Scan(&ready)
					if err != nil {
						return err
					}
				}
			}
			var deliveredAt any
			if ready {
				deliveredAt = time.Now().UTC()
			}
			_, err = q.Exec(ctx, `INSERT INTO sys_crypto_expected_transfer_deliveries(expected_effect_id,next_attempt_at,delivered_at,attempts) VALUES($1,now()+interval '30 seconds',$2,1)
    ON CONFLICT(expected_effect_id) DO UPDATE SET next_attempt_at=EXCLUDED.next_attempt_at,delivered_at=COALESCE(sys_crypto_expected_transfer_deliveries.delivered_at,EXCLUDED.delivered_at),attempts=sys_crypto_expected_transfer_deliveries.attempts+1,version=sys_crypto_expected_transfer_deliveries.version+1,_txid=txid_current()`, value, deliveredAt)
			return err
		})
		if err != nil {
			return delivered, err
		}
		if ready {
			delivered++
		}
	}
	return delivered, nil
}
