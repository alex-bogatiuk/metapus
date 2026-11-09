package crypto_worker

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"metapus/internal/application/outboundapp"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

func isPreEffectWithdrawalError(err error) bool { return outboundapp.IsPreEffectWithdrawalError(err) }
func withdrawalRecoveryReason(err error) string { return outboundapp.WithdrawalRecoveryReason(err) }

func (e *WithdrawalExecutor) observeRecoveryQueue(ctx context.Context) {
	count, age, err := crypto_repo.NewWorkerRuntimeRepo().WithdrawalRecoveryQueue(ctx)
	if err != nil {
		e.log.Errorw("observe withdrawal recovery queue", "error", err)
		return
	}
	attrs := metric.WithAttributes(attribute.String("tenant_id", e.tenantID))
	cryptoMetrics.withdrawalRecoveryCount.Record(ctx, count, attrs)
	cryptoMetrics.withdrawalRecoveryAge.Record(ctx, max(0, age), attrs)
}

// recoverSignedWithdrawal observes immutable signed bytes and finishes local
// bookkeeping after finality. AML can retry those same bytes under a fresh
// dispatch permit. Receipt-only legacy recovery ignores approval expiry.
