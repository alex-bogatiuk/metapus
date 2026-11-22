package crypto_repo

import (
	"context"
	"fmt"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

type WorkerInvoiceReconciliationCandidate struct {
	ID    id.ID
	Drift bool
}

func (r *WorkerRuntimeRepo) InvoiceProjectionHasDrift(ctx context.Context, invoiceID id.ID) (bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var drift bool
	err := q.QueryRow(ctx, `
		SELECT ci.received_amount IS DISTINCT FROM COALESCE(s.received_amount,0)
		    OR ci.confirmed_amount IS DISTINCT FROM COALESCE(s.confirmed_amount,0)
		    OR ci.credited_amount IS DISTINCT FROM COALESCE(s.credited_amount,0)
		    OR ci.status IS DISTINCT FROM expected.status
		    OR NOT (
		        EXISTS (SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.created')
		        AND (ci.status NOT IN ('paid','confirmed','completed') OR EXISTS (
		            SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.paid'
		        ))
		        AND (ci.status NOT IN ('confirmed','completed') OR EXISTS (
		            SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.confirmed'
		        ))
		        AND (ci.status<>'completed' OR EXISTS (
		            SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.completed'
		        ))
		        AND (ci.status<>'reversed' OR EXISTS (
		            SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.reversed'
		        ))
		        AND (ci.status<>'underpaid' OR EXISTS (
		            SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.underpaid'
		        ))
		        AND (ci.status<>'expired' OR EXISTS (
		            SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.expired'
		        ))
		        AND (ci.status<>'cancelled' OR EXISTS (
		            SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.cancelled'
		        ))
		    )
		FROM doc_crypto_invoices ci
		LEFT JOIN LATERAL (
			SELECT
				COALESCE(SUM(amount) FILTER (WHERE canonicality='canonical' AND status IN ('detected','confirming','confirmed','settled')),0) received_amount,
				COALESCE(SUM(amount) FILTER (WHERE canonicality='canonical' AND status IN ('confirmed','settled')),0) confirmed_amount,
				COALESCE(SUM(amount) FILTER (WHERE canonicality='canonical' AND posted AND status IN ('confirmed','settled')),0) credited_amount
			FROM doc_crypto_payments WHERE invoice_id=ci.id AND deletion_mark=FALSE
		) s ON TRUE
		CROSS JOIN LATERAL (
			SELECT CASE
				WHEN ci.status IN ('underpaid','expired','cancelled') THEN ci.status
				WHEN ci.single_payment AND ci.selected_payment_id IS NOT NULL AND COALESCE(s.credited_amount,0)>0 AND COALESCE(s.credited_amount,0)<ci.expected_amount THEN 'underpaid'
				WHEN ci.status IN ('confirmed','completed','reversed') AND COALESCE(s.confirmed_amount,0)<ci.expected_amount THEN 'reversed'
				WHEN COALESCE(s.credited_amount,0)>=ci.expected_amount THEN 'completed'
				WHEN COALESCE(s.confirmed_amount,0)>=ci.expected_amount THEN 'confirmed'
				WHEN COALESCE(s.received_amount,0)>=ci.expected_amount THEN 'paid'
				WHEN COALESCE(s.received_amount,0)>0 THEN 'partially_paid'
				ELSE 'created'
			END AS status
		) expected
		WHERE ci.id=$1 AND ci.deletion_mark=FALSE
	`, invoiceID).Scan(&drift)
	if err != nil {
		return false, fmt.Errorf("verify invoice projection repair: %w", err)
	}
	return drift, nil
}

func (r *WorkerRuntimeRepo) LoadInvoiceReconciliationCandidates(ctx context.Context, daily bool) ([]WorkerInvoiceReconciliationCandidate, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		SELECT ci.id,
		       ci.received_amount IS DISTINCT FROM COALESCE(s.received_amount,0)
		       OR ci.confirmed_amount IS DISTINCT FROM COALESCE(s.confirmed_amount,0)
		       OR ci.credited_amount IS DISTINCT FROM COALESCE(s.credited_amount,0)
		       OR ci.status IS DISTINCT FROM expected.status
		       OR NOT (
		           EXISTS (SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.created')
		           AND (ci.status NOT IN ('paid','confirmed','completed') OR EXISTS (
		               SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.paid'
		           ))
		           AND (ci.status NOT IN ('confirmed','completed') OR EXISTS (
		               SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.confirmed'
		           ))
		           AND (ci.status<>'completed' OR EXISTS (
		               SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.completed'
		           ))
		           AND (ci.status<>'reversed' OR EXISTS (
		               SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.reversed'
		           ))
		           AND (ci.status<>'underpaid' OR EXISTS (
		               SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.underpaid'
		           ))
		           AND (ci.status<>'expired' OR EXISTS (
		               SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.expired'
		           ))
		           AND (ci.status<>'cancelled' OR EXISTS (
		               SELECT 1 FROM sys_merchant_events me WHERE me.invoice_id=ci.id AND me.event_type='invoice.cancelled'
		           ))
		       ) AS drift
		FROM doc_crypto_invoices ci
		LEFT JOIN LATERAL (
			SELECT
				COALESCE(SUM(amount) FILTER (WHERE canonicality='canonical' AND status IN ('detected','confirming','confirmed','settled')),0) received_amount,
				COALESCE(SUM(amount) FILTER (WHERE canonicality='canonical' AND status IN ('confirmed','settled')),0) confirmed_amount,
				COALESCE(SUM(amount) FILTER (WHERE canonicality='canonical' AND posted AND status IN ('confirmed','settled')),0) credited_amount
			FROM doc_crypto_payments WHERE invoice_id=ci.id AND deletion_mark=FALSE
		) s ON TRUE
		CROSS JOIN LATERAL (
			SELECT CASE
				WHEN ci.status IN ('underpaid','expired','cancelled') THEN ci.status
				WHEN ci.single_payment AND ci.selected_payment_id IS NOT NULL AND COALESCE(s.credited_amount,0)>0 AND COALESCE(s.credited_amount,0)<ci.expected_amount THEN 'underpaid'
				WHEN ci.status IN ('confirmed','completed','reversed') AND COALESCE(s.confirmed_amount,0)<ci.expected_amount THEN 'reversed'
				WHEN COALESCE(s.credited_amount,0)>=ci.expected_amount THEN 'completed'
				WHEN COALESCE(s.confirmed_amount,0)>=ci.expected_amount THEN 'confirmed'
				WHEN COALESCE(s.received_amount,0)>=ci.expected_amount THEN 'paid'
				WHEN COALESCE(s.received_amount,0)>0 THEN 'partially_paid'
				ELSE 'created'
			END AS status
		) expected
		WHERE ci.deletion_mark=FALSE AND (
			ci.status IN ('created','partially_paid','paid','confirmed','reversed')
			OR ($1 AND (ci.completed_at >= NOW()-INTERVAL '30 days' OR
			              (ci.status='underpaid' AND ci.updated_at >= NOW()-INTERVAL '30 days')))
		)
		ORDER BY ci.updated_at ASC
		LIMIT 5000
	`, daily)
	if err != nil {
		return nil, fmt.Errorf("load invoice reconciliation candidates: %w", err)
	}
	defer rows.Close()
	items := make([]WorkerInvoiceReconciliationCandidate, 0, 256)
	for rows.Next() {
		var item WorkerInvoiceReconciliationCandidate
		if err := rows.Scan(&item.ID, &item.Drift); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *WorkerRuntimeRepo) CleanupMerchantWebhookRetention(ctx context.Context) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	// Delivery bodies and attempts are retained for 90 days. Jobs are deleted
	// first because merchant events are their immutable parent records.
	if _, err := q.Exec(ctx, `DELETE FROM sys_webhook_deliveries WHERE created_at < NOW()-INTERVAL '90 days'`); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `DELETE FROM sys_webhook_delivery_jobs WHERE created_at < NOW()-INTERVAL '90 days'`); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `DELETE FROM sys_merchant_events WHERE created_at < NOW()-INTERVAL '90 days'`); err != nil {
		return err
	}
	return nil
}

func (r *WorkerRuntimeRepo) RecoverMerchantWebhookClaims(ctx context.Context) (int64, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `
		UPDATE sys_webhook_delivery_jobs
		SET status='pending',updated_at=clock_timestamp(),claim_token=NULL,lease_until=NULL
		WHERE status='processing' AND lease_until<=clock_timestamp()
	`); err != nil {
		return 0, err
	}
	var queueAge int64
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(EXTRACT(EPOCH FROM NOW()-MIN(next_attempt_at)),0)::BIGINT
		FROM sys_webhook_delivery_jobs
		WHERE status='pending' AND next_attempt_at<=NOW()
	`).Scan(&queueAge); err != nil {
		return 0, err
	}

	return queueAge, nil
}

func (r *WorkerRuntimeRepo) EnqueueMissingMerchantWebhooks(ctx context.Context) error {
	// Events whose delivery message is still on its way are not lost. Every
	// merchant event has the merchant webhook aggregate type.
	pending, err := postgres.PendingOutboxPayloadValues(ctx, crypto.MerchantWebhookAggregateType, "id")
	if err != nil {
		return err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		SELECT me.aggregate_type,me.aggregate_id,me.event_type,me.payload,
		       me.traceparent,COALESCE(NULLIF(me.correlation_id,''),me.event_id),me.causation_key
		FROM sys_merchant_events me
		WHERE me.created_at >= NOW()-INTERVAL '30 days'
		  AND NOT EXISTS (
			SELECT 1
			FROM doc_crypto_invoices ci
			JOIN cat_tokens t ON t.id=ci.token_id
			LEFT JOIN sys_crypto_network_policies np
			  ON np.network_id=t.network_id AND np._deleted_at IS NULL
			WHERE ci.id=me.invoice_id
			  AND (
				np.network_id IS NULL OR np.enabled=FALSE OR EXISTS (
					SELECT 1 FROM reg_crypto_network_holds h
					WHERE h.network_id=t.network_id AND h.status='open'
				)
			  )
		  )
		  AND (
				NOT EXISTS (SELECT 1 FROM sys_webhook_delivery_jobs j WHERE j.event_id=me.event_id)
				AND (
					EXISTS (
						SELECT 1 FROM cat_merchant_webhook_endpoints e
						WHERE e.merchant_id=me.merchant_id AND e.is_active
						  AND e.deletion_mark=FALSE AND me.event_type=ANY(e.enabled_events)
					)
					OR EXISTS (
						SELECT 1 FROM doc_crypto_invoices ci
						JOIN cat_merchant_webhook_endpoints e
						  ON e.merchant_id=ci.merchant_id AND e.is_default AND e.is_active AND e.deletion_mark=FALSE
						WHERE ci.id=me.invoice_id AND ci.notification_url<>'' AND me.event_type LIKE 'invoice.%'
					)
				)
		  )
		  AND me.event_id<>ALL($1::text[])
		LIMIT 500
	`, pending)
	if err != nil {
		return fmt.Errorf("find merchant events without delivery: %w", err)
	}
	defer rows.Close()
	var lost []postgres.OutboxRecord
	for rows.Next() {
		var m postgres.OutboxRecord
		if err := rows.Scan(&m.AggregateType, &m.AggregateID, &m.EventType, &m.Payload, &m.Traceparent, &m.CorrelationID, &m.CausationID); err != nil {
			return fmt.Errorf("scan merchant event without delivery: %w", err)
		}
		lost = append(lost, m)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("find merchant events without delivery: %w", err)
	}
	return postgres.EnqueueOutbox(ctx, lost...)
}
