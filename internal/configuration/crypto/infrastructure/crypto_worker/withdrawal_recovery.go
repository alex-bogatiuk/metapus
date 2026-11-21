package crypto_worker

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"metapus/internal/configuration/crypto/application/outboundapp"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
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
