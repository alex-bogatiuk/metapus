package crypto_repo

import (
	"context"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"strings"
)

// RetryExhaustedAMLPreflight grants one new finite read cycle after an operator
// investigates an RPC outage. All observations and holds remain; this cannot
// override detected inventory or produce an empty baseline by itself.
func (r *AMLRepository) RetryExhaustedAMLPreflight(ctx context.Context, job id.ID, reason, actor string) error {
	if id.IsNil(job) || len(strings.TrimSpace(reason)) < 3 || len(reason) > 500 || strings.TrimSpace(actor) == "" || len(actor) > 100 {
		return apperror.NewValidation("preflight retry requires job, reason and operator")
	}
	tm := postgres.MustGetTxManager(ctx)
	return tm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := tm.GetQuerier(ctx)
		_, err := q.Exec(ctx, `SELECT s.version FROM reg_crypto_aml_preflight_jobs j JOIN cat_wallets w ON w.id=j.wallet_id JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=w.network_id AND s.token_id=j.token_id AND s.enabled WHERE j.id=$1 FOR UPDATE OF s`, job)
		if err != nil {
			return err
		}
		tag, err := q.Exec(ctx, `WITH exhausted AS (
   SELECT j.id,j.attempts,j.error_code FROM reg_crypto_aml_preflight_jobs j
   WHERE j.id=$1 AND j.status='needs_attention' AND j.attempts=120 AND j.error_code IN ('checkpoint_unavailable','checkpoint_rejected','checkpoint_attempts_exhausted')
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_observations o WHERE o.wallet_id=j.wallet_id AND o.token_id=j.token_id AND o.chain_amount<>0)
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines b WHERE b.wallet_id=j.wallet_id AND b.token_id=j.token_id)
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects e WHERE e.wallet_id=j.wallet_id AND e.token_id=j.token_id)
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements m WHERE m.wallet_id=j.wallet_id AND m.token_id=j.token_id)
   FOR UPDATE OF j
  ), audit AS (
   INSERT INTO sys_event_log(category,severity,event_type,source,user_id,entity_type,entity_id,message,details)
   SELECT 'system','info','crypto.aml.preflight_retry_authorized','aml-control',$3,'AMLPreflight',id,$2,jsonb_build_object('previousAttempts',attempts,'previousErrorCode',error_code,'newCycleLimit',120,'holdsPreserved',TRUE)
   FROM exhausted RETURNING entity_id
  ) UPDATE reg_crypto_aml_preflight_jobs j SET status='retry_wait',attempts=0,next_attempt_at=NOW(),error_code='',lease_token=NULL,lease_expires_at=NULL
   FROM audit WHERE j.id=audit.entity_id`, job, reason, actor)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return apperror.NewConflict("preflight is not an exhausted empty-wallet RPC retry")
		}
		return nil
	})
}
