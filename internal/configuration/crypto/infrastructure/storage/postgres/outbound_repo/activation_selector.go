package outbound_repo

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

var errActivationCandidateBusy = errors.New("activation candidate unavailable")

const activationPolicyEligible = `p.enabled AND NOT p.needs_review AND p.max_concurrent_activations IS NOT NULL AND p.activation_reserve_minor IS NOT NULL AND p.min_hot_native_balance_minor IS NOT NULL AND p.daily_activation_budget_minor IS NOT NULL`

// Planning is read-only. Both existing and new candidates must already have a
// usable payer/native asset before any task or wallet projection is changed.
const activationCandidateSQL = `WITH candidates AS (
 SELECT a.id,a.wallet_id,a.network_id,a.network_operation_id,a.status,a.updated_at sort_at FROM reg_crypto_account_activations a
 JOIN cat_wallets w ON w.id=a.wallet_id
 WHERE a.network_id=$1 AND a.status IN ('claimed','signing','broadcast','confirmed','held') AND a.activation_next_attempt_at<=clock_timestamp()
 AND (a.activation_claim_token IS NULL OR a.activation_lease_expires_at<=clock_timestamp()) AND w.chain_activation_status<>'active' AND w.is_active AND NOT w.deletion_mark
 AND (w.wallet_role='merchant_pool' OR EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions v JOIN reg_crypto_aml_pool_intents i ON i.id=v.id WHERE v.wallet_id=w.id AND v.status='completed'))
 UNION ALL
 SELECT NULL::uuid,w.id,w.network_id,NULL::uuid,'claimed',w.created_at FROM cat_wallets w
 WHERE w.network_id=$1 AND w.tier='pool' AND w.wallet_role='merchant_pool' AND w.is_active AND NOT w.deletion_mark
 AND w.chain_activation_status='unknown' AND w.address<>'' AND w.vault_key_id<>''
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions v JOIN reg_crypto_aml_pool_intents i ON i.id=v.id WHERE v.wallet_id=w.id)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=w.id AND status='open')
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_account_activations a WHERE a.wallet_id=w.id AND a.network_id=w.network_id AND a.status IN ('claimed','signing','broadcast','confirmed','held'))
)
 SELECT c.id,c.wallet_id,c.network_id,c.network_operation_id,c.status,n.code,target.address,COALESCE(o.tx_hash,''),h.id,h.address,h.vault_key_id,COALESCE(b.amount,0),nt.id
 FROM candidates c JOIN cat_wallets target ON target.id=c.wallet_id AND target.network_id=c.network_id
 JOIN sys_crypto_wallet_pool_policies p ON p.network_id=c.network_id JOIN cat_blockchain_networks n ON n.id=c.network_id
 LEFT JOIN doc_crypto_network_operations o ON o.id=c.network_operation_id
 LEFT JOIN LATERAL(SELECT a.source_address,a.vault_key_id FROM reg_crypto_signing_authorizations a WHERE a.basis_type='CryptoAccountActivationTask' AND a.basis_id=c.id AND a.purpose='activation' ORDER BY a.created_at DESC LIMIT 1) prior ON TRUE
 JOIN LATERAL(SELECT w.id,w.address,w.vault_key_id FROM cat_wallets w WHERE w.network_id=c.network_id AND w.tier='hot' AND w.address<>'' AND w.vault_key_id<>''
 AND ((o.id IS NOT NULL AND w.id=o.source_wallet_id)
 OR (o.id IS NULL AND prior.source_address IS NOT NULL AND w.address=prior.source_address AND w.vault_key_id=prior.vault_key_id)
 OR (o.id IS NULL AND prior.source_address IS NULL AND w.wallet_role IN ('liquidity_hot','network_fee') AND w.is_active AND NOT w.deletion_mark
 AND w.custody_merchant_id IS NOT DISTINCT FROM target.custody_merchant_id
 AND (w.wallet_role='network_fee' OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s JOIN cat_tokens t ON t.id=s.token_id AND t.token_standard='native' WHERE s.merchant_id=target.custody_merchant_id AND s.network_id=c.network_id AND s.enabled))
 AND (target.custody_merchant_id IS NOT NULL OR NOT EXISTS(SELECT 1 FROM sys_crypto_custody_policies cp WHERE cp.network_id=c.network_id AND cp.enforced)))) ORDER BY w.code,w.id LIMIT 1) h ON TRUE
 JOIN LATERAL(SELECT t.id FROM cat_tokens t WHERE t.network_id=c.network_id AND t.token_standard='native' AND t.is_active AND NOT t.deletion_mark ORDER BY t.code,t.id LIMIT 1) nt ON TRUE
 LEFT JOIN reg_crypto_balance_balances b ON b.wallet_id=h.id AND b.token_id=nt.id
 WHERE ` + activationPolicyEligible + `
 AND (c.network_operation_id IS NULL OR (o.network_id=c.network_id AND o.target_wallet_id=c.wallet_id AND o.basis_type='CryptoAccountActivationTask' AND o.basis_id=c.id))
 AND ($2::uuid IS NULL OR c.wallet_id=$2) ORDER BY (c.id IS NULL),c.sort_at,c.wallet_id LIMIT $3`

type activationCandidate struct {
	task  AccountActivationTask
	fresh bool
}

func activationCandidates(ctx context.Context, network id.ID, target *id.ID, limit int) ([]activationCandidate, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, activationCandidateSQL, network, target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []activationCandidate
	for rows.Next() {
		var c activationCandidate
		var key *id.ID
		if err := rows.Scan(&key, &c.task.WalletID, &c.task.NetworkID, &c.task.NetworkOperationID, &c.task.Status, &c.task.NetworkCode, &c.task.Address, &c.task.TxHash, &c.task.HotWallet.ID, &c.task.HotWallet.Address, &c.task.HotWallet.VaultKeyID, &c.task.HotWallet.Balance, &c.task.CostTokenID); err != nil {
			return nil, err
		}
		c.fresh = key == nil
		if key == nil {
			c.task.ActivationID = id.New()
		} else {
			c.task.ActivationID = *key
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

func (r *OutboundRepo) ClaimAccountActivationTasks(ctx context.Context, worker string, limit int) ([]AccountActivationTask, error) {
	if limit <= 0 {
		limit = _outboundBatchLimit
	}
	if worker == "" {
		return nil, apperror.NewValidation("activation execution requires a worker")
	}
	tm := postgres.MustGetTxManager(ctx)
	var tasks []AccountActivationTask
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var isolation string
		if err := q.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
			return err
		}
		if isolation != "read committed" {
			return apperror.NewValidation("activation selection requires read committed isolation")
		}
		rows, err := q.Query(ctx, `SELECT p.network_id FROM sys_crypto_wallet_pool_policies p WHERE `+activationPolicyEligible+` ORDER BY p.network_id`)
		if err != nil {
			return err
		}
		var networks []id.ID
		for rows.Next() {
			var key id.ID
			if err := rows.Scan(&key); err != nil {
				rows.Close()
				return err
			}
			networks = append(networks, key)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, network := range networks {
			if len(tasks) >= limit {
				break
			}
			var acquired bool
			if err := q.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1::text,104195))`, network).Scan(&acquired); err != nil {
				return err
			}
			if !acquired {
				continue
			}
			candidates, err := activationCandidates(ctx, network, nil, max(100, min(limit, 250)*4))
			if err != nil {
				return err
			}
			for _, candidate := range candidates {
				if len(tasks) >= limit {
					break
				}
				var claimed AccountActivationTask
				err := tm.RunInSavepoint(ctx, func(ctx context.Context) error {
					var err error
					claimed, err = claimActivationCandidate(ctx, candidate, worker)
					return err
				})
				var pgerr *pgconn.PgError
				if errors.Is(err, errActivationCandidateBusy) || (errors.As(err, &pgerr) && pgerr.Code == "55P03") {
					continue
				}
				if err != nil {
					return err
				}
				tasks = append(tasks, claimed)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claim account activations: %w", err)
	}
	return tasks, nil
}

func claimActivationCandidate(ctx context.Context, c activationCandidate, worker string) (AccountActivationTask, error) {
	t := c.task
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	locker := postgres.NewDocLocker()
	for _, key := range []struct {
		kind string
		key  *id.ID
	}{{crypto_network_operation.ActivationTaskBasisType, &t.ActivationID}, {crypto_network_operation.DocumentType, t.NetworkOperationID}} {
		if key.key == nil {
			continue
		}
		acquired, err := locker.TryLockDocument(ctx, key.kind, *key.key)
		if err != nil {
			return t, err
		}
		if !acquired {
			return t, errActivationCandidateBusy
		}
	}
	var policy id.ID
	if err := q.QueryRow(ctx, `SELECT p.network_id FROM sys_crypto_wallet_pool_policies p WHERE p.network_id=$1 AND `+activationPolicyEligible+` FOR UPDATE NOWAIT`, t.NetworkID).Scan(&policy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return t, errActivationCandidateBusy
		}
		return t, err
	}
	var wallets []id.ID
	if err := q.QueryRow(ctx, activationWalletSetSQL, t.ActivationID, t.NetworkOperationID, []id.ID{t.WalletID, t.HotWallet.ID}).Scan(&wallets); err != nil {
		return t, err
	}
	var acquired bool
	if err := q.QueryRow(ctx, `SELECT fn_crypto_try_lock_custody_wallets($1::uuid[])`, wallets).Scan(&acquired); err != nil {
		return t, err
	}
	if !acquired {
		return t, errActivationCandidateBusy
	}
	if !c.fresh {
		var key id.ID
		if err := q.QueryRow(ctx, `SELECT id FROM reg_crypto_account_activations WHERE id=$1 FOR UPDATE NOWAIT`, t.ActivationID).Scan(&key); err != nil {
			return t, err
		}
	}
	fresh, err := activationCandidates(ctx, t.NetworkID, &t.WalletID, 1)
	if err != nil {
		return t, err
	}
	if len(fresh) != 1 || fresh[0].fresh != c.fresh || (!c.fresh && fresh[0].task.ActivationID != t.ActivationID) {
		return t, errActivationCandidateBusy
	}
	n := fresh[0].task
	if n.HotWallet.ID != t.HotWallet.ID || n.CostTokenID != t.CostTokenID || (n.NetworkOperationID == nil) != (t.NetworkOperationID == nil) || (n.NetworkOperationID != nil && *n.NetworkOperationID != *t.NetworkOperationID) {
		return t, errActivationCandidateBusy
	}
	var current []id.ID
	if err := q.QueryRow(ctx, activationWalletSetSQL, t.ActivationID, t.NetworkOperationID, []id.ID{t.WalletID, t.HotWallet.ID}).Scan(&current); err != nil {
		return t, err
	}
	if !slices.Equal(wallets, current) {
		return t, errActivationCandidateBusy
	}
	if c.fresh {
		var capacity bool
		if err := q.QueryRow(ctx, `SELECT p.target_ready>
 (SELECT count(*) FROM cat_wallets w WHERE w.network_id=$1 AND w.tier='pool' AND w.wallet_role='merchant_pool' AND w.status='free' AND w.allocation_mode='transient' AND w.chain_activation_status='active' AND w.is_active AND NOT w.deletion_mark AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=w.id AND status='open'))
 +(SELECT count(*) FROM reg_crypto_account_activations a JOIN cat_wallets w ON w.id=a.wallet_id WHERE a.network_id=$1 AND a.status IN ('claimed','signing','broadcast','confirmed','held') AND w.wallet_role='merchant_pool' AND w.is_active AND NOT w.deletion_mark)
 FROM sys_crypto_wallet_pool_policies p WHERE p.network_id=$1`, t.NetworkID).Scan(&capacity); err != nil {
			return t, err
		}
		if !capacity {
			return t, errActivationCandidateBusy
		}
		tag, err := q.Exec(ctx, `INSERT INTO reg_crypto_account_activations(id,wallet_id,network_id,status) VALUES($1,$2,$3,'claimed') ON CONFLICT DO NOTHING`, t.ActivationID, t.WalletID, t.NetworkID)
		if err != nil {
			return t, err
		}
		if tag.RowsAffected() != 1 {
			return t, errActivationCandidateBusy
		}
	}
	if err := q.QueryRow(ctx, `UPDATE reg_crypto_account_activations SET status=CASE WHEN status='held' THEN 'claimed' ELSE status END,
 error_message=CASE WHEN status='held' THEN '' ELSE error_message END,activation_attempts=activation_attempts+1,activation_claimed_by=$2,activation_claim_token=gen_random_uuid_v7(),activation_lease_expires_at=clock_timestamp()+($3*interval '1 second'),activation_next_attempt_at=clock_timestamp()+($3*interval '1 second'),updated_at=now()
 WHERE id=$1 RETURNING status,activation_claim_token`, t.ActivationID, worker, int64(_accountActivationLease.Seconds())).Scan(&n.Status, &n.ClaimToken); err != nil {
		return t, err
	}
	if _, err := q.Exec(ctx, `UPDATE cat_wallets SET chain_activation_status='activating',updated_at=now(),version=version+1 WHERE id=$1 AND chain_activation_status<>'active'`, t.WalletID); err != nil {
		return t, err
	}
	n.ActivationID = t.ActivationID
	return n, nil
}
