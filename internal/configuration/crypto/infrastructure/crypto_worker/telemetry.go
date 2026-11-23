package crypto_worker

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

var cryptoProcessingTracer = otel.Tracer("github.com/alex-bogatiuk/metapus/crypto-processing")

type cryptoProcessingInstruments struct {
	executionAttempts       metric.Int64Counter
	executionOutcomes       metric.Int64Counter
	signerChecks            metric.Int64Counter
	signingOperations       metric.Int64Counter
	resourceDecisions       metric.Int64Counter
	feeReserved             metric.Int64Counter
	feeActual               metric.Int64Counter
	budgetRemaining         metric.Int64Histogram
	nativeFunding           metric.Int64Counter
	ambiguousBroadcasts     metric.Int64Counter
	settlementLagSeconds    metric.Int64Histogram
	runtimeReadiness        metric.Int64Counter
	replacementEvents       metric.Int64Counter
	reconciliationRuns      metric.Int64Counter
	invoiceReconciliation   metric.Int64Counter
	walletCooldown          metric.Int64Gauge
	walletReady             metric.Int64Gauge
	chainEventInbox         metric.Int64Counter
	latePayments            metric.Int64Counter
	watcherLagSeconds       metric.Int64Gauge
	leaseBlocked            metric.Int64Gauge
	webhookDispatchLag      metric.Int64Histogram
	webhookAttempts         metric.Int64Counter
	webhookRetryAge         metric.Int64Histogram
	webhookTerminal         metric.Int64Counter
	webhookQueueAge         metric.Int64Gauge
	invoiceTransitionLag    metric.Int64Histogram
	withdrawalRecoveryCount metric.Int64Gauge
	withdrawalRecoveryAge   metric.Int64Gauge
}

var cryptoMetrics = newCryptoProcessingInstruments()

func newCryptoProcessingInstruments() cryptoProcessingInstruments {
	meter := otel.Meter("github.com/alex-bogatiuk/metapus/crypto-processing")
	executionAttempts, _ := meter.Int64Counter("crypto.execution.attempts")
	executionOutcomes, _ := meter.Int64Counter("crypto.execution.outcomes")
	signerChecks, _ := meter.Int64Counter("crypto.signer.readiness_checks")
	signingOperations, _ := meter.Int64Counter("crypto.signing.operations")
	resourceDecisions, _ := meter.Int64Counter("crypto.resource.decisions")
	feeReserved, _ := meter.Int64Counter("crypto.fee.reserved_minor")
	feeActual, _ := meter.Int64Counter("crypto.fee.actual_minor")
	budgetRemaining, _ := meter.Int64Histogram("crypto.fee.budget_remaining_minor")
	nativeFunding, _ := meter.Int64Counter("crypto.native_funding.amount_minor")
	ambiguousBroadcasts, _ := meter.Int64Counter("crypto.broadcast.ambiguous")
	settlementLagSeconds, _ := meter.Int64Histogram("crypto.settlement.lag_seconds")
	runtimeReadiness, _ := meter.Int64Counter("crypto.runtime.readiness_observations")
	replacementEvents, _ := meter.Int64Counter("crypto.evm.replacement.events")
	reconciliationRuns, _ := meter.Int64Counter("crypto.reconciliation.runs")
	invoiceReconciliation, _ := meter.Int64Counter("crypto.invoice_reconciliation.cycles")
	walletCooldown, _ := meter.Int64Gauge("crypto.wallet.cooldown_count")
	walletReady, _ := meter.Int64Gauge("crypto.wallet.ready_count")
	chainEventInbox, _ := meter.Int64Counter("crypto.chain_event.inbox")
	latePayments, _ := meter.Int64Counter("crypto.payment.late")
	watcherLagSeconds, _ := meter.Int64Gauge("crypto.watcher.lag_seconds")
	leaseBlocked, _ := meter.Int64Gauge("crypto.wallet.lease_blocked")
	webhookDispatchLag, _ := meter.Int64Histogram("crypto.webhook.event_to_dispatch_latency_seconds")
	webhookAttempts, _ := meter.Int64Counter("crypto.webhook.delivery_attempts")
	webhookRetryAge, _ := meter.Int64Histogram("crypto.webhook.retry_age_seconds")
	webhookTerminal, _ := meter.Int64Counter("crypto.webhook.terminal_failures")
	webhookQueueAge, _ := meter.Int64Gauge("crypto.webhook.queue_age_seconds")
	invoiceTransitionLag, _ := meter.Int64Histogram("crypto.invoice.transition_lag_seconds")
	withdrawalRecoveryCount, _ := meter.Int64Gauge("crypto.withdrawal.recovery_count")
	withdrawalRecoveryAge, _ := meter.Int64Gauge("crypto.withdrawal.recovery_oldest_age_seconds")
	return cryptoProcessingInstruments{
		executionAttempts: executionAttempts, executionOutcomes: executionOutcomes,
		signerChecks: signerChecks, signingOperations: signingOperations,
		resourceDecisions: resourceDecisions, feeReserved: feeReserved, feeActual: feeActual,
		budgetRemaining: budgetRemaining, nativeFunding: nativeFunding,
		ambiguousBroadcasts: ambiguousBroadcasts, settlementLagSeconds: settlementLagSeconds,
		runtimeReadiness: runtimeReadiness, replacementEvents: replacementEvents,
		reconciliationRuns:    reconciliationRuns,
		invoiceReconciliation: invoiceReconciliation,
		walletCooldown:        walletCooldown, walletReady: walletReady,
		chainEventInbox: chainEventInbox, latePayments: latePayments,
		watcherLagSeconds: watcherLagSeconds, leaseBlocked: leaseBlocked,
		webhookDispatchLag: webhookDispatchLag, webhookAttempts: webhookAttempts,
		webhookRetryAge: webhookRetryAge, webhookTerminal: webhookTerminal,
		webhookQueueAge: webhookQueueAge, invoiceTransitionLag: invoiceTransitionLag,
		withdrawalRecoveryCount: withdrawalRecoveryCount, withdrawalRecoveryAge: withdrawalRecoveryAge,
	}
}

func recordInboxMetric(ctx context.Context, status, reason string) {
	cryptoMetrics.chainEventInbox.Add(ctx, 1, metric.WithAttributes(
		attribute.String("match_status", status),
		attribute.String("reason", reason),
	))
}

func recordLatePaymentMetric(ctx context.Context) {
	cryptoMetrics.latePayments.Add(ctx, 1)
}

func startExecutionSpan(ctx context.Context, name string, networkID id.ID, purpose crypto.SigningPurpose) (context.Context, trace.Span) {
	attrs := executionAttributes(networkID, purpose)
	ctx, span := cryptoProcessingTracer.Start(ctx, name, trace.WithAttributes(attrs...))
	cryptoMetrics.executionAttempts.Add(ctx, 1, metric.WithAttributes(attrs...))
	return ctx, span
}

func finishExecutionSpan(ctx context.Context, span trace.Span, networkID id.ID, purpose crypto.SigningPurpose, err error) {
	outcome := "succeeded"
	if err != nil {
		outcome = "failed"
		if isHeldExecutionError(err) {
			outcome = "held"
		}
		span.SetStatus(codes.Error, outcome)
	} else {
		span.SetStatus(codes.Ok, outcome)
	}
	attrs := append(executionAttributes(networkID, purpose), attribute.String("crypto.outcome", outcome))
	cryptoMetrics.executionOutcomes.Add(ctx, 1, metric.WithAttributes(attrs...))
	span.End()
}

func startCryptoPhase(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return cryptoProcessingTracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

func finishCryptoPhase(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(codes.Error, "failed")
	} else {
		span.SetStatus(codes.Ok, "succeeded")
	}
	span.End()
}

func executionAttributes(networkID id.ID, purpose crypto.SigningPurpose) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 2)
	if !id.IsNil(networkID) {
		attrs = append(attrs, attribute.String("crypto.network.id", networkID.String()))
	}
	if purpose != "" {
		attrs = append(attrs, attribute.String("crypto.purpose", string(purpose)))
	}
	return attrs
}

func networkAttributes(network crypto.NetworkRef) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("crypto.network.id", network.ID.String()),
		attribute.String("crypto.network.code", network.Code),
		attribute.String("crypto.chain.family", string(network.Family)),
	}
}

func recordSignerCheck(ctx context.Context, family crypto.ChainFamily, ready bool) {
	cryptoMetrics.signerChecks.Add(ctx, 1, metric.WithAttributes(
		attribute.String("crypto.chain.family", string(family)),
		attribute.Bool("crypto.ready", ready),
	))
}

func recordSigningOperation(ctx context.Context, family crypto.ChainFamily, operation string, reused bool, err error) {
	cryptoMetrics.signingOperations.Add(ctx, 1, metric.WithAttributes(
		attribute.String("crypto.chain.family", string(family)),
		attribute.String("crypto.operation", operation),
		attribute.Bool("crypto.reused", reused),
		attribute.Bool("crypto.succeeded", err == nil),
	))
}

func recordResourceDecision(ctx context.Context, networkID id.ID, policy crypto.ResourceStrategyPolicy, strategy crypto.FeeStrategy, err error) {
	decision := string(strategy)
	if decision == "" {
		decision = "held"
	}
	cryptoMetrics.resourceDecisions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("crypto.network.id", networkID.String()),
		attribute.String("crypto.resource.policy", string(policy)),
		attribute.String("crypto.resource.decision", decision),
		attribute.Bool("crypto.succeeded", err == nil),
	))
}

func recordFeeReservation(ctx context.Context, req FeeReservationRequest, err error, remaining *types.RawAmount) {
	attrs := []attribute.KeyValue{
		attribute.String("crypto.network.id", req.NetworkID.String()),
		attribute.String("crypto.purpose", string(req.Purpose)),
		attribute.String("crypto.fee.strategy", string(req.Strategy)),
		attribute.Bool("crypto.succeeded", err == nil),
	}
	if err == nil {
		if amount, conversionErr := req.MaxFee.Int64Exact(); conversionErr == nil {
			cryptoMetrics.feeReserved.Add(ctx, amount, metric.WithAttributes(attrs...))
		}
	}
	if remaining != nil {
		if amount, conversionErr := remaining.Int64Exact(); conversionErr == nil {
			cryptoMetrics.budgetRemaining.Record(ctx, amount, metric.WithAttributes(attrs...))
		}
	}
}

func recordFeeSettlement(ctx context.Context, receipt crypto.ChainReceipt, purpose crypto.SigningPurpose, reservedAt time.Time, err error) {
	attrs := []attribute.KeyValue{
		attribute.String("crypto.network.id", receipt.NetworkID.String()),
		attribute.String("crypto.purpose", string(purpose)),
		attribute.String("crypto.receipt.status", string(receipt.Status)),
		attribute.Bool("crypto.succeeded", err == nil),
	}
	if err == nil {
		if amount, conversionErr := receipt.Fee.TotalFee.Int64Exact(); conversionErr == nil {
			cryptoMetrics.feeActual.Add(ctx, amount, metric.WithAttributes(attrs...))
		}
	}
	if !reservedAt.IsZero() && !receipt.ObservedAt.IsZero() {
		lag := max(int64(receipt.ObservedAt.Sub(reservedAt)/time.Second), 0)
		cryptoMetrics.settlementLagSeconds.Record(ctx, lag, metric.WithAttributes(attrs...))
	}
}

func recordNativeFunding(ctx context.Context, networkID id.ID, amount types.RawAmount) {
	if value, err := amount.Int64Exact(); err == nil {
		cryptoMetrics.nativeFunding.Add(ctx, value, metric.WithAttributes(
			attribute.String("crypto.network.id", networkID.String()),
		))
	}
}

func recordBroadcastOutcome(ctx context.Context, network crypto.NetworkRef, purpose crypto.SigningPurpose, outcome crypto.BroadcastOutcome) {
	if outcome.Status != crypto.BroadcastAmbiguous {
		return
	}
	attrs := append(networkAttributes(network), attribute.String("crypto.purpose", string(purpose)))
	cryptoMetrics.ambiguousBroadcasts.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func recordRuntimeReadiness(ctx context.Context, network crypto.NetworkRef, component string, ready bool) {
	attrs := append(networkAttributes(network),
		attribute.String("crypto.component", component),
		attribute.Bool("crypto.ready", ready),
	)
	cryptoMetrics.runtimeReadiness.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func recordReplacementEvent(ctx context.Context, networkID id.ID, generation int, event string) {
	cryptoMetrics.replacementEvents.Add(ctx, 1, metric.WithAttributes(
		attribute.String("crypto.network.id", networkID.String()),
		attribute.Int("crypto.generation", generation),
		attribute.String("crypto.replacement.event", event),
	))
}

func recordReconciliationRun(ctx context.Context, result ReconciliationResult, err error) {
	cryptoMetrics.reconciliationRuns.Add(ctx, 1, metric.WithAttributes(
		attribute.Bool("crypto.acquired", result.Acquired),
		attribute.Bool("crypto.succeeded", err == nil),
		attribute.Int("crypto.read_errors", result.ReadErrors),
		attribute.Int("crypto.eligible_pairs", result.EligiblePairs),
		attribute.Int("crypto.attempted_pairs", result.AttemptedPairs),
		attribute.Int("crypto.successful_pairs", result.WalletsChecked),
		attribute.Int("crypto.items_created", result.ItemsCreated),
		attribute.Int("crypto.items_updated", result.ItemsUpdated),
	))
}

func recordInvoiceReconciliation(ctx context.Context, checked, repairs, failures int, daily bool) {
	cryptoMetrics.invoiceReconciliation.Add(ctx, 1, metric.WithAttributes(
		attribute.Int("crypto.invoices_checked", checked),
		attribute.Int("crypto.invoice_repairs", repairs),
		attribute.Int("crypto.invoice_repair_failures", failures),
		attribute.Bool("crypto.daily_scan", daily),
	))
}

func recordWebhookAttemptStart(ctx context.Context, eventType crypto.WebhookEventType, attempt int, manual bool, occurredAt, startedAt time.Time) {
	age := max(int64(startedAt.Sub(occurredAt)/time.Second), 0)
	attrs := []attribute.KeyValue{
		attribute.String("crypto.webhook.event_type", string(eventType)),
		attribute.Int("crypto.webhook.attempt", attempt),
		attribute.Bool("crypto.webhook.manual", manual),
	}
	cryptoMetrics.webhookDispatchLag.Record(ctx, age, metric.WithAttributes(attrs...))
	cryptoMetrics.invoiceTransitionLag.Record(ctx, age, metric.WithAttributes(attrs...))
	if attempt > 1 {
		cryptoMetrics.webhookRetryAge.Record(ctx, age, metric.WithAttributes(attrs...))
	}
}

func recordWebhookAttemptOutcome(ctx context.Context, eventType crypto.WebhookEventType, attempt int, manual, succeeded, terminal bool) {
	attrs := []attribute.KeyValue{
		attribute.String("crypto.webhook.event_type", string(eventType)),
		attribute.Int("crypto.webhook.attempt", attempt),
		attribute.Bool("crypto.webhook.manual", manual),
		attribute.Bool("crypto.webhook.succeeded", succeeded),
		attribute.Bool("crypto.webhook.first_attempt", attempt == 1),
	}
	cryptoMetrics.webhookAttempts.Add(ctx, 1, metric.WithAttributes(attrs...))
	if terminal && !succeeded {
		cryptoMetrics.webhookTerminal.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func recordWebhookQueueAge(ctx context.Context, seconds int64) {
	cryptoMetrics.webhookQueueAge.Record(ctx, max(seconds, 0))
}
