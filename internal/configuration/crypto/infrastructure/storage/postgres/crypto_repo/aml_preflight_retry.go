package crypto_repo

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/eventlog"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
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
		var attempts int
		var errorCode string
		err = q.QueryRow(ctx, `WITH exhausted AS (
   SELECT j.id,j.attempts,j.error_code FROM reg_crypto_aml_preflight_jobs j
   WHERE j.id=$1 AND j.status='needs_attention' AND j.attempts=120 AND j.error_code IN ('checkpoint_unavailable','checkpoint_rejected','checkpoint_attempts_exhausted')
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_observations o WHERE o.wallet_id=j.wallet_id AND o.token_id=j.token_id AND o.chain_amount<>0)
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines b WHERE b.wallet_id=j.wallet_id AND b.token_id=j.token_id)
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects e WHERE e.wallet_id=j.wallet_id AND e.token_id=j.token_id)
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements m WHERE m.wallet_id=j.wallet_id AND m.token_id=j.token_id)
   FOR UPDATE OF j
  ) UPDATE reg_crypto_aml_preflight_jobs j SET status='retry_wait',attempts=0,next_attempt_at=NOW(),error_code='',lease_token=NULL,lease_expires_at=NULL
   FROM exhausted WHERE j.id=exhausted.id
   RETURNING exhausted.attempts,exhausted.error_code`, job).Scan(&attempts, &errorCode)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NewConflict("preflight is not an exhausted empty-wallet RPC retry")
		}
		if err != nil {
			return err
		}
		return postgres.NewEventLogRepo().Write(ctx, eventlog.Event{
			Category: eventlog.CategorySystem, Severity: eventlog.SeverityInfo,
			EventType: "crypto.aml.preflight_retry_authorized", Source: "aml-control", UserID: actor,
			EntityType: "AMLPreflight", EntityID: &job, Message: reason,
			Details: map[string]any{"previousAttempts": attempts, "previousErrorCode": errorCode, "newCycleLimit": 120, "holdsPreserved": true},
		})
	})
}
