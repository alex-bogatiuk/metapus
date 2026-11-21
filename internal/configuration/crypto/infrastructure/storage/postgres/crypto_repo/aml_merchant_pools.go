package crypto_repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

const merchantPoolColumns = `p.id,p.merchant_id,p.network_id,p.wallet_role,p.token_id,COALESCE(p.risk_class,''),p.enabled,p.version,p.low_watermark,p.target_ready,p.max_total,p.max_provision_batch,p.automatic_baseline`

func scanMerchantPool(row rowScanner) (crypto.MerchantPoolPolicy, error) {
	var p crypto.MerchantPoolPolicy
	err := row.Scan(&p.ID, &p.MerchantID, &p.NetworkID, &p.Role, &p.TokenID, &p.RiskClass, &p.Enabled, &p.Version, &p.LowWatermark, &p.TargetReady, &p.MaxTotal, &p.MaxProvisionBatch, &p.AutomaticBaseline)
	return p, err
}

func lockMerchantPoolNetwork(ctx context.Context, networkID id.ID) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var locked id.ID
	if err := q.QueryRow(ctx, `SELECT network_id FROM sys_crypto_custody_policies WHERE network_id=$1 AND enforced FOR UPDATE`, networkID).Scan(&locked); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `SELECT token_id FROM sys_crypto_aml_routing_scopes WHERE network_id=$1 ORDER BY merchant_id,network_id,token_id FOR UPDATE`, networkID)
	return err
}

func (r *AMLRepository) ConfigureMerchantPoolPolicy(ctx context.Context, request crypto.MerchantPoolPolicyRequest) (crypto.MerchantPoolPolicy, error) {
	var result crypto.MerchantPoolPolicy
	if err := request.Validate(); err != nil {
		return result, err
	}
	p := request.Policy
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var previousNetwork id.ID
		err := q.QueryRow(ctx, `SELECT network_id FROM sys_crypto_aml_pool_policies WHERE id=$1`, p.ID).Scan(&previousNetwork)
		if err == nil && previousNetwork != p.NetworkID {
			return apperror.NewConflict("merchant pool scope is immutable")
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := lockMerchantPoolNetwork(ctx, p.NetworkID); err != nil {
			return err
		}
		current, err := scanMerchantPool(q.QueryRow(ctx, `SELECT `+merchantPoolColumns+` FROM sys_crypto_aml_pool_policies p WHERE p.id=$1 FOR UPDATE`, p.ID))
		var token any
		if p.TokenID != nil {
			token = *p.TokenID
		}
		if errors.Is(err, pgx.ErrNoRows) {
			if request.ExpectedVersion != 0 {
				return apperror.NewConflict("merchant pool policy version changed")
			}
			_, err = q.Exec(ctx, `INSERT INTO sys_crypto_aml_pool_policies(id,merchant_id,network_id,wallet_role,token_id,risk_class,enabled,low_watermark,target_ready,max_total,max_provision_batch,configured_by,automatic_baseline) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12,$13)`, p.ID, p.MerchantID, p.NetworkID, p.Role, token, p.RiskClass, p.Enabled, p.LowWatermark, p.TargetReady, p.MaxTotal, p.MaxProvisionBatch, request.Actor, p.AutomaticBaseline)
		} else if err == nil {
			if current.Version != request.ExpectedVersion {
				return apperror.NewConflict("merchant pool policy version changed")
			}
			_, err = q.Exec(ctx, `UPDATE sys_crypto_aml_pool_policies SET merchant_id=$2,network_id=$3,wallet_role=$4,token_id=$5,risk_class=NULLIF($6,''),enabled=$7,low_watermark=$8,target_ready=$9,max_total=$10,max_provision_batch=$11,configured_by=$12,automatic_baseline=$13,version=version+1,next_reconcile_at=NOW(),last_reconcile_error='' WHERE id=$1`, p.ID, p.MerchantID, p.NetworkID, p.Role, token, p.RiskClass, p.Enabled, p.LowWatermark, p.TargetReady, p.MaxTotal, p.MaxProvisionBatch, request.Actor, p.AutomaticBaseline)
		}
		if err != nil {
			return err
		}
		result, err = scanMerchantPool(q.QueryRow(ctx, `SELECT `+merchantPoolColumns+` FROM sys_crypto_aml_pool_policies p WHERE p.id=$1`, p.ID))
		return err
	})
	return result, err
}

func (r *AMLRepository) ReadMerchantPoolStats(ctx context.Context, merchantID, policyID id.ID) (crypto.MerchantPoolStats, error) {
	var stats crypto.MerchantPoolStats
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT usage.ready,usage.pending,usage.queued,usage.total,usage.started FROM sys_crypto_aml_pool_policies p CROSS JOIN LATERAL fn_crypto_aml_pool_counts(p.id) usage WHERE p.id=$1 AND p.merchant_id=$2`, policyID, merchantID).Scan(&stats.Ready, &stats.Pending, &stats.Queued, &stats.Total, &stats.Started)
	return stats, err
}

// ReconcileNextMerchantPool reserves finite address capacity before derivation.
// Scope/network locks serialize different policy rows sharing one HD budget.
func (r *AMLRepository) ReconcileNextMerchantPool(ctx context.Context) (bool, int, error) {
	progressed := false
	created := 0
	var planningErr error
	tm := postgres.MustGetTxManager(ctx)
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var policyID, networkID id.ID
		err := q.QueryRow(ctx, `SELECT p.id,p.network_id FROM sys_crypto_aml_pool_policies p JOIN crypto_aml_pool_runtime runtime ON runtime.id=p.id JOIN sys_crypto_custody_policies custody ON custody.network_id=p.network_id AND custody.enforced
 WHERE p.enabled AND p.next_reconcile_at<=NOW() ORDER BY p.next_reconcile_at,p.id FOR UPDATE OF custody SKIP LOCKED LIMIT 1`).Scan(&policyID, &networkID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := lockMerchantPoolNetwork(ctx, networkID); err != nil {
			return err
		}
		policy, err := scanMerchantPool(q.QueryRow(ctx, `SELECT `+merchantPoolColumns+` FROM sys_crypto_aml_pool_policies p WHERE p.id=$1 FOR UPDATE`, policyID))
		if err != nil {
			return err
		}
		planningErr = tm.RunInSavepoint(ctx, func(ctx context.Context) error {
			stats, err := r.ReadMerchantPoolStats(ctx, policy.MerchantID, policyID)
			if err != nil {
				return err
			}
			var remaining int
			if err := q.QueryRow(ctx, `SELECT GREATEST(max_total_wallets-fn_crypto_aml_network_wallet_commitments(network_id,NULL),0) FROM sys_crypto_wallet_pool_policies WHERE network_id=$1 AND enabled AND NOT needs_review FOR UPDATE`, networkID).Scan(&remaining); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				return err
			}
			count := crypto.PlanMerchantPoolRefill(policy, stats, remaining)
			round := id.New()
			for ordinal := 1; ordinal <= count; ordinal++ {
				if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_pool_intents(policy_id,policy_version,plan_id,ordinal) VALUES($1,$2,$3,$4)`, policyID, policy.Version, round, ordinal); err != nil {
					return err
				}
			}
			created = count
			return nil
		})
		message := ""
		if planningErr != nil {
			created = 0
			message = "pool_capacity_evidence_conflict"
		}
		_, err = q.Exec(ctx, `UPDATE sys_crypto_aml_pool_policies SET next_reconcile_at=NOW()+interval '15 seconds',last_reconcile_error=$2 WHERE id=$1`, policyID, message)
		progressed = true
		return err
	})
	return progressed, created, errors.Join(err, planningErr)
}

func (r *AMLRepository) merchantPoolAdmission(ctx context.Context, request crypto.MerchantWalletRequest) (crypto.MerchantPoolPolicy, crypto.MerchantPoolStats, bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	policy, err := scanMerchantPool(q.QueryRow(ctx, `SELECT `+merchantPoolColumns+` FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id WHERE i.id=$1 FOR UPDATE OF p,i`, request.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return policy, crypto.MerchantPoolStats{}, false, nil
	}
	if err != nil {
		return policy, crypto.MerchantPoolStats{}, true, err
	}
	if policy.MerchantID != request.MerchantID || policy.NetworkID != request.NetworkID || policy.Role != request.Role || request.Actor != crypto.MerchantPoolActor {
		return policy, crypto.MerchantPoolStats{}, true, apperror.NewConflict("merchant pool intent identity changed")
	}
	var cancelled bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intent_cancellations WHERE intent_id=$1)`, request.ID).Scan(&cancelled); err != nil {
		return policy, crypto.MerchantPoolStats{}, true, err
	}
	if cancelled {
		return policy, crypto.MerchantPoolStats{}, true, apperror.NewConflict("merchant pool intent was cancelled")
	}
	var runtimeReady bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM crypto_aml_pool_runtime WHERE id=$1)`, policy.ID).Scan(&runtimeReady); err != nil {
		return policy, crypto.MerchantPoolStats{}, true, err
	}
	policy.Enabled = policy.Enabled && runtimeReady
	stats, err := r.ReadMerchantPoolStats(ctx, policy.MerchantID, policy.ID)
	return policy, stats, true, err
}

func (r *AMLRepository) ClaimNextMerchantPoolProvision(ctx context.Context, worker string, lease time.Duration) (crypto.MerchantWalletProvision, bool, error) {
	var plan crypto.MerchantWalletProvision
	claimed := false
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) != nil {
		return plan, false, apperror.NewConflict("merchant provisioning cannot run inside a SQL transaction")
	}
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		request := crypto.MerchantWalletRequest{Actor: crypto.MerchantPoolActor}
		err := q.QueryRow(ctx, `SELECT i.id,p.merchant_id,p.network_id,p.wallet_role FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.enabled JOIN crypto_aml_pool_runtime runtime ON runtime.id=p.id
 JOIN sys_crypto_custody_policies custody ON custody.network_id=p.network_id AND custody.enforced
 JOIN sys_crypto_wallet_pool_policies base ON base.network_id=p.network_id AND base.enabled AND NOT base.needs_review
 LEFT JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id CROSS JOIN LATERAL fn_crypto_aml_pool_counts(p.id) usage
 WHERE NOT EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intent_cancellations cancel WHERE cancel.intent_id=i.id)
 AND ((v.id IS NULL AND usage.started<p.max_total AND usage.ready+usage.pending<p.target_ready)
 OR (v.status='retry_wait' AND v.next_attempt_at<=NOW()) OR (v.status='processing' AND v.lease_expires_at<=NOW()))
 ORDER BY i.created_at,i.id FOR UPDATE OF custody SKIP LOCKED LIMIT 1`).Scan(&request.ID, &request.MerchantID, &request.NetworkID, &request.Role)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		plan, err = r.claimMerchantWalletProvision(ctx, request, worker, lease)
		claimed = err == nil
		return err
	})
	return plan, claimed, err
}

// OpenNextReadyMerchantPoolBatch uses previously approved empty baselines. It
// never fabricates a checkpoint or clears a reconciliation/activation hold.
func (r *AMLRepository) OpenNextReadyMerchantPoolBatch(ctx context.Context) (bool, error) {
	progressed := false
	var openingErr error
	tm := postgres.MustGetTxManager(ctx)
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var intentID, networkID, policyID id.ID
		err := q.QueryRow(ctx, `SELECT i.id,p.network_id,p.id FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.enabled AND p.wallet_role='liquidity_hot' JOIN crypto_aml_pool_runtime runtime ON runtime.id=p.id
 JOIN sys_crypto_custody_policies custody ON custody.network_id=p.network_id AND custody.enforced
 JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed' JOIN cat_wallets w ON w.id=v.wallet_id AND w.is_active AND NOT w.deletion_mark AND w.chain_activation_status='active'
 JOIN reg_crypto_aml_wallet_baselines baseline ON baseline.wallet_id=w.id AND baseline.token_id=p.token_id
 JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=w.id AND inventory.token_id=p.token_id AND inventory.principal+inventory.service_fee=0
 WHERE i.next_batch_attempt_at<=NOW() AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_batches b WHERE b.collector_wallet_id=w.id)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds hold WHERE hold.wallet_id=w.id AND hold.status='open')
 AND fn_crypto_aml_total_inventory_covered(w.id,p.token_id)
 ORDER BY i.next_batch_attempt_at,i.created_at,i.id FOR UPDATE OF custody SKIP LOCKED LIMIT 1`).Scan(&intentID, &networkID, &policyID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := lockMerchantPoolNetwork(ctx, networkID); err != nil {
			return err
		}
		policy, err := scanMerchantPool(q.QueryRow(ctx, `SELECT `+merchantPoolColumns+` FROM sys_crypto_aml_pool_policies p WHERE p.id=$1 FOR UPDATE`, policyID))
		if err != nil {
			return err
		}
		openingErr = tm.RunInSavepoint(ctx, func(ctx context.Context) error {
			var collector id.ID
			var scopeVersion, policyVersion int64
			if err := q.QueryRow(ctx, `SELECT v.wallet_id,s.version,(SELECT MAX(version) FROM sys_crypto_aml_batch_policies WHERE merchant_id=p.merchant_id AND network_id=p.network_id AND token_id=p.token_id AND risk_class=p.risk_class)
 FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.enabled JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id
 JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=p.token_id AND s.enabled WHERE i.id=$1 FOR UPDATE OF i`, intentID).Scan(&collector, &scopeVersion, &policyVersion); err != nil {
				return err
			}
			_, err := r.OpenAMLBatch(ctx, aml.OpenBatchRequest{ID: intentID, MerchantID: policy.MerchantID, NetworkID: networkID, TokenID: *policy.TokenID, CollectorID: collector, Class: aml.Class(policy.RiskClass), PolicyVersion: policyVersion, ScopeVersion: scopeVersion, Actor: crypto.MerchantPoolActor})
			return err
		})
		if openingErr != nil {
			_, err = q.Exec(ctx, `UPDATE reg_crypto_aml_pool_intents SET batch_attempts=batch_attempts+1,batch_error='collector_preflight_conflict',next_batch_attempt_at=NOW()+interval '30 seconds' WHERE id=$1`, intentID)
		}
		progressed = true
		return err
	})
	return progressed, errors.Join(err, openingErr)
}

func (r *AMLRepository) CancelMerchantPoolIntent(ctx context.Context, intentID id.ID, reason, actor string) error {
	if id.IsNil(intentID) || len(strings.TrimSpace(reason)) < 3 || len(reason) > 500 || strings.TrimSpace(actor) == "" {
		return apperror.NewValidation("pool cancellation requires intent, reason and actor")
	}
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var isolation string
		if err := q.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
			return err
		}
		if isolation != "read committed" {
			return apperror.NewValidation("pool cancellation requires read committed isolation")
		}
		var networkID id.ID
		if err := q.QueryRow(ctx, `SELECT p.network_id FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id WHERE i.id=$1`, intentID).Scan(&networkID); err != nil {
			return err
		}
		if err := lockMerchantPoolNetwork(ctx, networkID); err != nil {
			return err
		}
		var exact bool
		err := q.QueryRow(ctx, `SELECT reason=$2 AND cancelled_by=$3 FROM reg_crypto_aml_pool_intent_cancellations WHERE intent_id=$1`, intentID, reason, actor).Scan(&exact)
		if err == nil {
			if !exact {
				return apperror.NewConflict("pool cancellation replay differs from its original command")
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_pool_intent_cancellations(intent_id,reason,cancelled_by) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, intentID, reason, actor)
		if err != nil {
			return err
		}
		if err := q.QueryRow(ctx, `SELECT reason=$2 AND cancelled_by=$3 FROM reg_crypto_aml_pool_intent_cancellations WHERE intent_id=$1`, intentID, reason, actor).Scan(&exact); err != nil {
			return err
		}
		if !exact {
			return apperror.NewConflict("pool cancellation replay differs from its original command")
		}
		return nil
	})
}

// ReleaseNextMerchantPoolBaselineHold rechecks every currently enabled asset.
// Independent checks remain necessary; neither zero SQL balance nor derivation
// proves a chain checkpoint. The loop also handles concurrent baseline commits.
func (r *AMLRepository) ReleaseNextMerchantPoolBaselineHold(ctx context.Context) (bool, error) {
	progressed := false
	tm := postgres.MustGetTxManager(ctx)
	err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		var intentID, networkID, walletID id.ID
		err := q.QueryRow(ctx, `SELECT i.id,p.network_id,v.wallet_id FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id
 JOIN sys_crypto_custody_policies custody ON custody.network_id=p.network_id AND custody.enforced JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed'
 WHERE EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds hold WHERE hold.wallet_id=v.wallet_id AND hold.status='open' AND hold.owner_type IN ('aml_pool_baseline','aml_pool_scope_baseline'))
 AND fn_crypto_aml_pool_baselines_ready(i.id) ORDER BY i.created_at,i.id FOR UPDATE OF custody SKIP LOCKED LIMIT 1`).Scan(&intentID, &networkID, &walletID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := lockMerchantPoolNetwork(ctx, networkID); err != nil {
			return err
		}
		tag, err := q.Exec(ctx, `UPDATE reg_crypto_wallet_allocation_holds SET status='resolved',resolved_at=NOW(),resolved_by='aml-pool-controller',resolution='all enabled asset baselines are independently registered and covered'
 WHERE wallet_id=$1 AND status='open' AND owner_type IN ('aml_pool_baseline','aml_pool_scope_baseline') AND fn_crypto_aml_pool_baselines_ready($2)`, walletID, intentID)
		progressed = tag.RowsAffected() > 0
		return err
	})
	return progressed, err
}
