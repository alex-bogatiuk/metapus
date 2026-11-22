package crypto_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/telemetry"
	"metapus/pkg/sdk/workerjob"
)

type MerchantWebhookRepo struct{}

func NewMerchantWebhookRepo() *MerchantWebhookRepo { return &MerchantWebhookRepo{} }

func webhookQuerier(ctx context.Context) postgres.Querier {
	return postgres.MustGetTxManager(ctx).GetQuerier(ctx)
}

func (r *MerchantWebhookRepo) CreateEndpoint(ctx context.Context, endpoint *crypto.MerchantWebhookEndpoint) error {
	if id.IsNil(endpoint.ID) {
		endpoint.ID = id.New()
	}
	if endpoint.Version < 1 {
		endpoint.Version = 1
	}
	now := time.Now().UTC()
	endpoint.CreatedAt, endpoint.UpdatedAt = now, now
	events := webhookEventStrings(endpoint.EnabledEvents)
	q := webhookQuerier(ctx)
	if endpoint.IsDefault {
		if _, err := q.Exec(ctx, `UPDATE cat_merchant_webhook_endpoints SET is_default=FALSE,version=version+1 WHERE merchant_id=$1 AND is_default AND deletion_mark=FALSE`, endpoint.MerchantID); err != nil {
			return fmt.Errorf("replace default merchant webhook endpoint: %w", err)
		}
	}
	_, err := q.Exec(ctx, `
		INSERT INTO cat_merchant_webhook_endpoints(
			id,merchant_id,name,url,enabled_events,is_default,is_active,
			secret_ref,secret_version,version,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)
	`, endpoint.ID, endpoint.MerchantID, endpoint.Name, endpoint.URL, events,
		endpoint.IsDefault, endpoint.IsActive, endpoint.SecretRef, endpoint.SecretVersion, endpoint.Version, now)
	if err != nil {
		return fmt.Errorf("create merchant webhook endpoint: %w", err)
	}
	return nil
}

func (r *MerchantWebhookRepo) GetEndpoint(ctx context.Context, merchantID, endpointID id.ID) (*crypto.MerchantWebhookEndpoint, error) {
	const query = `
		SELECT id,merchant_id,name,url,enabled_events,is_default,is_active,
		       secret_ref,secret_version,previous_secret_ref,previous_secret_version,
		       previous_secret_valid_until,version,created_at,updated_at
		FROM cat_merchant_webhook_endpoints
		WHERE id=$1 AND merchant_id=$2 AND deletion_mark=FALSE`
	return scanWebhookEndpoint(ctx, webhookQuerier(ctx).QueryRow(ctx, query, endpointID, merchantID))
}

func (r *MerchantWebhookRepo) ListEndpoints(ctx context.Context, merchantID id.ID) ([]crypto.MerchantWebhookEndpoint, error) {
	rows, err := webhookQuerier(ctx).Query(ctx, `
		SELECT id,merchant_id,name,url,enabled_events,is_default,is_active,
		       secret_ref,secret_version,previous_secret_ref,previous_secret_version,
		       previous_secret_valid_until,version,created_at,updated_at
		FROM cat_merchant_webhook_endpoints WHERE merchant_id=$1 AND deletion_mark=FALSE
		ORDER BY is_default DESC,created_at ASC
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("list merchant webhook endpoints: %w", err)
	}
	defer rows.Close()
	result := make([]crypto.MerchantWebhookEndpoint, 0)
	for rows.Next() {
		endpoint, err := scanWebhookEndpoint(ctx, rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *endpoint)
	}
	return result, rows.Err()
}

func (r *MerchantWebhookRepo) UpdateEndpoint(ctx context.Context, endpoint *crypto.MerchantWebhookEndpoint) error {
	q := webhookQuerier(ctx)
	if endpoint.IsDefault && endpoint.IsActive {
		if _, err := q.Exec(ctx, `UPDATE cat_merchant_webhook_endpoints SET is_default=FALSE,version=version+1 WHERE merchant_id=$1 AND id<>$2 AND is_default AND deletion_mark=FALSE`, endpoint.MerchantID, endpoint.ID); err != nil {
			return fmt.Errorf("replace default merchant webhook endpoint: %w", err)
		}
	}
	result, err := q.Exec(ctx, `
		UPDATE cat_merchant_webhook_endpoints
		SET name=$3,url=$4,enabled_events=$5,is_default=$6,is_active=$7,version=version+1
		WHERE id=$1 AND merchant_id=$2 AND version=$8 AND deletion_mark=FALSE
	`, endpoint.ID, endpoint.MerchantID, endpoint.Name, endpoint.URL,
		webhookEventStrings(endpoint.EnabledEvents), endpoint.IsDefault, endpoint.IsActive, endpoint.Version)
	if err != nil {
		return fmt.Errorf("update merchant webhook endpoint: %w", err)
	}
	if result.RowsAffected() == 0 {
		return apperror.NewConcurrentModification("webhook_endpoint", endpoint.ID.String())
	}
	endpoint.Version++
	return nil
}

func (r *MerchantWebhookRepo) DeleteEndpoint(ctx context.Context, merchantID, endpointID id.ID) error {
	q := webhookQuerier(ctx)
	var lockedID id.ID
	if err := q.QueryRow(ctx, `
		SELECT id FROM cat_merchant_webhook_endpoints
		WHERE id=$1 AND merchant_id=$2 AND deletion_mark=FALSE FOR UPDATE
	`, endpointID, merchantID).Scan(&lockedID); err != nil {
		if err == pgx.ErrNoRows {
			return apperror.NewNotFound("webhook endpoint", endpointID.String())
		}
		return fmt.Errorf("lock merchant webhook endpoint for deletion: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO sys_webhook_secret_retirements(secret_ref,retire_after)
		SELECT secret_ref,NOW()+INTERVAL '73 hours'
		FROM (
			SELECT secret_ref FROM cat_merchant_webhook_endpoints WHERE id=$1 AND merchant_id=$2 AND deletion_mark=FALSE
			UNION
			SELECT previous_secret_ref FROM cat_merchant_webhook_endpoints
			WHERE id=$1 AND merchant_id=$2 AND deletion_mark=FALSE AND previous_secret_ref<>''
		) secrets
		ON CONFLICT (secret_ref) DO UPDATE
		SET retire_after=GREATEST(sys_webhook_secret_retirements.retire_after,EXCLUDED.retire_after),
		    claimed_until=NULL,updated_at=NOW()
	`, endpointID, merchantID); err != nil {
		return fmt.Errorf("schedule webhook endpoint secret retirement: %w", err)
	}
	result, err := q.Exec(ctx, `
		UPDATE cat_merchant_webhook_endpoints
		SET deletion_mark=TRUE,is_active=FALSE,is_default=FALSE,version=version+1
		WHERE id=$1 AND merchant_id=$2 AND deletion_mark=FALSE
	`, endpointID, merchantID)
	if err != nil {
		return fmt.Errorf("soft-delete merchant webhook endpoint: %w", err)
	}
	if result.RowsAffected() == 0 {
		return apperror.NewNotFound("webhook endpoint", endpointID.String())
	}
	return nil
}

func (r *MerchantWebhookRepo) HasDefaultEndpoint(ctx context.Context, merchantID id.ID) (bool, error) {
	var exists bool
	err := webhookQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM cat_merchant_webhook_endpoints
		WHERE merchant_id=$1 AND is_default AND is_active AND deletion_mark=FALSE
	)`, merchantID).Scan(&exists)
	return exists, err
}

func (r *MerchantWebhookRepo) RotateEndpointSecret(ctx context.Context, merchantID, endpointID id.ID, newRef string, now time.Time) (*crypto.MerchantWebhookEndpoint, error) {
	q := webhookQuerier(ctx)
	var lockedID id.ID
	if err := q.QueryRow(ctx, `
		SELECT id FROM cat_merchant_webhook_endpoints
		WHERE id=$1 AND merchant_id=$2 AND deletion_mark=FALSE FOR UPDATE
	`, endpointID, merchantID).Scan(&lockedID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("webhook endpoint", endpointID.String())
		}
		return nil, fmt.Errorf("lock merchant webhook endpoint for rotation: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO sys_webhook_secret_retirements(secret_ref,retire_after)
		SELECT secret_ref,$3::timestamptz+INTERVAL '73 hours'
		FROM (
			SELECT secret_ref FROM cat_merchant_webhook_endpoints WHERE id=$1 AND merchant_id=$2 AND deletion_mark=FALSE
			UNION
			SELECT previous_secret_ref FROM cat_merchant_webhook_endpoints
			WHERE id=$1 AND merchant_id=$2 AND deletion_mark=FALSE AND previous_secret_ref<>''
		) secrets
		ON CONFLICT (secret_ref) DO UPDATE
		SET retire_after=GREATEST(sys_webhook_secret_retirements.retire_after,EXCLUDED.retire_after),
		    claimed_until=NULL,updated_at=NOW()
	`, endpointID, merchantID, now); err != nil {
		return nil, fmt.Errorf("schedule rotated webhook secret retirement: %w", err)
	}
	row := q.QueryRow(ctx, `
		UPDATE cat_merchant_webhook_endpoints
		SET previous_secret_ref=secret_ref,
		    previous_secret_version=secret_version,
		    previous_secret_valid_until=$4,
		    secret_ref=$3,secret_version=secret_version+1,version=version+1
		WHERE id=$1 AND merchant_id=$2 AND deletion_mark=FALSE
		RETURNING id,merchant_id,name,url,enabled_events,is_default,is_active,
		          secret_ref,secret_version,previous_secret_ref,previous_secret_version,
		          previous_secret_valid_until,version,created_at,updated_at
	`, endpointID, merchantID, newRef, now.Add(crypto.WebhookSecretRotationOverlap))
	return scanWebhookEndpoint(ctx, row)
}

func (r *MerchantWebhookRepo) ClaimDueSecretRetirements(ctx context.Context, now time.Time, limit int) ([]crypto.WebhookSecretRetirement, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	var records []crypto.WebhookSecretRetirement
	err := pgxscan.Select(ctx, webhookQuerier(ctx), &records, `
		WITH due AS (
			SELECT secret_ref FROM sys_webhook_secret_retirements
			WHERE retire_after<=$1 AND (claimed_until IS NULL OR claimed_until<=$1)
			ORDER BY retire_after,secret_ref LIMIT $2 FOR UPDATE SKIP LOCKED
		)
		UPDATE sys_webhook_secret_retirements r
		SET claimed_until=$1+INTERVAL '5 minutes',updated_at=$1
		FROM due WHERE r.secret_ref=due.secret_ref
		RETURNING r.secret_ref,r.attempt_count
	`, now, limit)
	return records, err
}

func (r *MerchantWebhookRepo) CompleteSecretRetirement(ctx context.Context, secretRef string) error {
	_, err := webhookQuerier(ctx).Exec(ctx, `DELETE FROM sys_webhook_secret_retirements WHERE secret_ref=$1`, secretRef)
	return err
}

func (r *MerchantWebhookRepo) RetrySecretRetirement(ctx context.Context, secretRef string, next time.Time, lastError string) error {
	_, err := webhookQuerier(ctx).Exec(ctx, `
		UPDATE sys_webhook_secret_retirements
		SET claimed_until=$2,attempt_count=attempt_count+1,last_error=$3,updated_at=NOW()
		WHERE secret_ref=$1
	`, secretRef, next, lastError)
	return err
}

func (r *MerchantWebhookRepo) ListEvents(ctx context.Context, merchantID id.ID, limit, offset int) ([]crypto.MerchantEventRecord, int, error) {
	q := webhookQuerier(ctx)
	var total int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM sys_merchant_events WHERE merchant_id=$1`, merchantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	var records []crypto.MerchantEventRecord
	if err := pgxscan.Select(ctx, q, &records, `
		SELECT id,event_id,merchant_id,invoice_id,event_type,api_version,resource_version,payload,created_at
		FROM sys_merchant_events WHERE merchant_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3
	`, merchantID, limit, offset); err != nil {
		return nil, 0, fmt.Errorf("list merchant events: %w", err)
	}
	return records, total, nil
}

func (r *MerchantWebhookRepo) GetEvent(ctx context.Context, merchantID id.ID, eventID string) (*crypto.MerchantEventRecord, error) {
	var record crypto.MerchantEventRecord
	if err := pgxscan.Get(ctx, webhookQuerier(ctx), &record, `
		SELECT id,event_id,merchant_id,invoice_id,event_type,api_version,resource_version,payload,created_at
		FROM sys_merchant_events WHERE merchant_id=$1 AND event_id=$2
	`, merchantID, eventID); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("merchant event", eventID)
		}
		return nil, err
	}
	return &record, nil
}

func (r *MerchantWebhookRepo) ListEventDeliveries(ctx context.Context, merchantID id.ID, eventID string) ([]crypto.WebhookDelivery, error) {
	var deliveries []crypto.WebhookDelivery
	err := pgxscan.Select(ctx, webhookQuerier(ctx), &deliveries, `
		SELECT id,invoice_id,merchant_id,event_type,webhook_url,event_id,delivery_id,
		       status_code,response_time_ms,attempt,error_message,error_class,response_body,
		       next_retry_at,request_body,created_at
		FROM sys_webhook_deliveries WHERE merchant_id=$1 AND event_id=$2 ORDER BY created_at DESC
	`, merchantID, eventID)
	return deliveries, err
}

func (r *MerchantWebhookRepo) PrepareAutomaticJobs(ctx context.Context, event crypto.LifecycleWebhookEvent) error {
	q := webhookQuerier(ctx)
	var notificationURL string
	if event.InvoiceID != nil {
		if err := q.QueryRow(ctx, `SELECT notification_url FROM doc_crypto_invoices WHERE id=$1`, *event.InvoiceID).Scan(&notificationURL); err != nil {
			return fmt.Errorf("load invoice notification override: %w", err)
		}
	}
	deadline := event.OccurredAt.Add(72 * time.Hour)
	if notificationURL != "" {
		if !crypto.IsBaseInvoiceEvent(event.Event) {
			return nil
		}
		_, err := q.Exec(ctx, `
			INSERT INTO sys_webhook_delivery_jobs(
				id,event_id,endpoint_id,merchant_id,snapshot_url,secret_ref,secret_version,
				previous_secret_ref,previous_secret_valid_until,next_attempt_at,deadline_at)
			SELECT gen_random_uuid_v7(),$1,e.id,$2,$3,e.secret_ref,e.secret_version,
			       e.previous_secret_ref,e.previous_secret_valid_until,$4,$5
			FROM cat_merchant_webhook_endpoints e
			WHERE e.merchant_id=$2 AND e.is_default AND e.is_active AND e.deletion_mark=FALSE
			ON CONFLICT (event_id,endpoint_id) WHERE manual=FALSE DO NOTHING
		`, event.ID, event.MerchantID, notificationURL, event.OccurredAt, deadline)
		return err
	}
	_, err := q.Exec(ctx, `
		INSERT INTO sys_webhook_delivery_jobs(
			id,event_id,endpoint_id,merchant_id,snapshot_url,secret_ref,secret_version,
			previous_secret_ref,previous_secret_valid_until,next_attempt_at,deadline_at)
		SELECT gen_random_uuid_v7(),$1,e.id,$2,e.url,e.secret_ref,e.secret_version,
		       e.previous_secret_ref,e.previous_secret_valid_until,$4,$5
		FROM cat_merchant_webhook_endpoints e
		WHERE e.merchant_id=$2 AND e.is_active AND e.deletion_mark=FALSE AND $3=ANY(e.enabled_events)
		ON CONFLICT (event_id,endpoint_id) WHERE manual=FALSE DO NOTHING
	`, event.ID, event.MerchantID, string(event.Event), event.OccurredAt, deadline)
	return err
}

func (r *MerchantWebhookRepo) ListDueJobs(ctx context.Context, eventID string, now time.Time) ([]crypto.WebhookDeliveryJob, bool, error) {
	q := webhookQuerier(ctx)
	var unfinished bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_webhook_delivery_jobs WHERE event_id=$1 AND status IN ('pending','processing'))`, eventID).Scan(&unfinished); err != nil {
		return nil, false, err
	}
	jobs, err := r.claimDueJobs(ctx, eventID, now, 1)
	return jobs, unfinished, err
}

func (r *MerchantWebhookRepo) ClaimDueJobs(ctx context.Context, now time.Time, limit int) ([]crypto.WebhookDeliveryJob, error) {
	return r.claimDueJobs(ctx, "", now, limit)
}

// Locking the merchant row while claiming and the partial unique index both
// enforce one in-flight HTTP attempt per merchant across all worker replicas.
func (r *MerchantWebhookRepo) claimDueJobs(ctx context.Context, eventID string, now time.Time, limit int) ([]crypto.WebhookDeliveryJob, error) {
	var jobs []crypto.WebhookDeliveryJob
	err := pgxscan.Select(ctx, webhookQuerier(ctx), &jobs, `
 WITH merchants AS (
  SELECT m.id FROM cat_merchants m
  WHERE EXISTS (SELECT 1 FROM sys_webhook_delivery_jobs j
    WHERE j.merchant_id=m.id AND j.status='pending' AND j.next_attempt_at<=$2
     AND ($1::text='' OR j.event_id=$1)
     AND NOT EXISTS (
      SELECT 1 FROM sys_merchant_events me
      JOIN doc_crypto_invoices ci ON ci.id=me.invoice_id
      JOIN cat_tokens t ON t.id=ci.token_id
      LEFT JOIN sys_crypto_network_policies np ON np.network_id=t.network_id AND np._deleted_at IS NULL
      WHERE me.event_id=j.event_id AND (np.network_id IS NULL OR np.enabled=FALSE OR EXISTS (
       SELECT 1 FROM reg_crypto_network_holds h WHERE h.network_id=t.network_id AND h.status='open'))
     ))
   AND NOT EXISTS (SELECT 1 FROM sys_webhook_delivery_jobs j WHERE j.merchant_id=m.id AND j.status='processing')
  ORDER BY COALESCE((SELECT j.updated_at FROM sys_webhook_delivery_jobs j
    WHERE j.merchant_id=m.id AND j.attempt_count>0 ORDER BY j.updated_at DESC LIMIT 1),'epoch'::timestamptz),m.id
  LIMIT $3 FOR UPDATE OF m SKIP LOCKED
 ), due AS (
  SELECT candidate.id FROM merchants m CROSS JOIN LATERAL (
   SELECT j.id FROM sys_webhook_delivery_jobs j
   WHERE j.merchant_id=m.id AND j.status='pending' AND j.next_attempt_at<=$2
    AND ($1::text='' OR j.event_id=$1)
    AND NOT EXISTS (
     SELECT 1 FROM sys_merchant_events me
     JOIN doc_crypto_invoices ci ON ci.id=me.invoice_id
     JOIN cat_tokens t ON t.id=ci.token_id
     LEFT JOIN sys_crypto_network_policies np ON np.network_id=t.network_id AND np._deleted_at IS NULL
     WHERE me.event_id=j.event_id AND (np.network_id IS NULL OR np.enabled=FALSE OR EXISTS (
      SELECT 1 FROM reg_crypto_network_holds h WHERE h.network_id=t.network_id AND h.status='open'))
    )
   ORDER BY j.next_attempt_at,j.created_at,j.id LIMIT 1 FOR UPDATE OF j SKIP LOCKED
  ) candidate
 )
 UPDATE sys_webhook_delivery_jobs j
 SET status='processing',claim_token=gen_random_uuid_v7(),lease_until=clock_timestamp()+INTERVAL '60 seconds',updated_at=clock_timestamp()
 FROM due,sys_merchant_events me WHERE j.id=due.id AND me.event_id=j.event_id
 RETURNING j.id,j.event_id,j.endpoint_id,j.merchant_id,j.snapshot_url,j.secret_ref,j.secret_version,
  j.previous_secret_ref,j.previous_secret_valid_until,j.attempt_count,j.deadline_at,j.manual,
  j.created_at AS schedule_started_at,j.claim_token,me.payload AS event_payload
 `, eventID, now, max(1, limit))
	return jobs, err
}

func (r *MerchantWebhookRepo) RenewJob(ctx context.Context, jobID, claimToken id.ID) error {
	result, err := webhookQuerier(ctx).Exec(ctx, `UPDATE sys_webhook_delivery_jobs
  SET lease_until=clock_timestamp()+INTERVAL '60 seconds'
  WHERE id=$1 AND status='processing' AND claim_token=$2 AND lease_until>clock_timestamp()`, jobID, claimToken)
	if err == nil && result.RowsAffected() != 1 {
		return workerjob.ErrLeaseLost
	}
	return err
}

func (r *MerchantWebhookRepo) MarkJobSucceeded(ctx context.Context, jobID, claimToken id.ID) error {
	result, err := webhookQuerier(ctx).Exec(ctx, `UPDATE sys_webhook_delivery_jobs SET status='succeeded',attempt_count=attempt_count+1,updated_at=clock_timestamp(),claim_token=NULL,lease_until=NULL WHERE id=$1 AND status='processing' AND claim_token=$2 AND lease_until>clock_timestamp()`, jobID, claimToken)
	if err == nil && result.RowsAffected() != 1 {
		return workerjob.ErrLeaseLost
	}
	return err
}

func (r *MerchantWebhookRepo) MarkJobRetry(ctx context.Context, jobID, claimToken id.ID, attempt int, next time.Time, terminal bool, lastError string) error {
	status := "pending"
	if terminal {
		status = "failed"
	}
	result, err := webhookQuerier(ctx).Exec(ctx, `
		UPDATE sys_webhook_delivery_jobs SET status=$2,attempt_count=$3,next_attempt_at=$4,last_error=$5,updated_at=clock_timestamp(),claim_token=NULL,lease_until=NULL WHERE id=$1 AND status='processing' AND claim_token=$6 AND lease_until>clock_timestamp()
	`, jobID, status, attempt, next, lastError, claimToken)
	if err == nil && result.RowsAffected() != 1 {
		return workerjob.ErrLeaseLost
	}
	return err
}

func (r *MerchantWebhookRepo) CreateManualJob(ctx context.Context, merchantID id.ID, eventID string, endpointID id.ID, now time.Time) error {
	q := webhookQuerier(ctx)
	result, err := q.Exec(ctx, `
		INSERT INTO sys_webhook_delivery_jobs(
			id,event_id,endpoint_id,merchant_id,snapshot_url,secret_ref,secret_version,
			previous_secret_ref,previous_secret_valid_until,next_attempt_at,deadline_at,manual,created_at,updated_at)
		SELECT gen_random_uuid_v7(),$1,e.id,$2,e.url,e.secret_ref,e.secret_version,
		       e.previous_secret_ref,e.previous_secret_valid_until,$4::timestamptz,$5::timestamptz,TRUE,$4::timestamptz,$4::timestamptz
		FROM cat_merchant_webhook_endpoints e
		JOIN sys_merchant_events me ON me.event_id=$1 AND me.merchant_id=$2
		WHERE e.id=$3 AND e.merchant_id=$2 AND e.is_active AND e.deletion_mark=FALSE
		  AND me.created_at >= $4::timestamptz-INTERVAL '30 days'
		  AND EXISTS (
		      SELECT 1
		      FROM sys_webhook_delivery_jobs failed
		      WHERE failed.event_id = me.event_id
		        AND failed.endpoint_id = e.id
		        AND failed.merchant_id = me.merchant_id
		        AND failed.manual = FALSE
		        AND failed.status = 'failed'
		  )
	`, eventID, merchantID, endpointID, now, now.Add(72*time.Hour))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var targetExists bool
		if err := q.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM cat_merchant_webhook_endpoints e
				JOIN sys_merchant_events me ON me.event_id=$1 AND me.merchant_id=$2
				WHERE e.id=$3
				  AND e.merchant_id=$2
				  AND e.is_active
				  AND e.deletion_mark=FALSE
				  AND me.created_at >= $4::timestamptz-INTERVAL '30 days'
			)
		`, eventID, merchantID, endpointID, now).Scan(&targetExists); err != nil {
			return err
		}
		if !targetExists {
			return apperror.NewNotFound("event or endpoint", eventID)
		}
		return apperror.NewConflict("manual redelivery is allowed only after a terminal automatic delivery failure")
	}
	var payload []byte
	var aggregateID id.ID
	var eventType string
	if err := q.QueryRow(ctx, `
		SELECT payload,aggregate_id,event_type
		FROM sys_merchant_events
		WHERE event_id=$1 AND merchant_id=$2
	`, eventID, merchantID).Scan(&payload, &aggregateID, &eventType); err != nil {
		return err
	}
	business := telemetry.BusinessIDs(ctx, eventID)
	return postgres.EnqueueOutbox(ctx, postgres.OutboxRecord{
		AggregateType: crypto.MerchantWebhookAggregateType, AggregateID: aggregateID, EventType: eventType,
		Payload: payload, CreatedAt: now,
		Traceparent: telemetry.Traceparent(ctx), CorrelationID: business.CorrelationID, CausationID: eventID,
	})
}

func scanWebhookEndpoint(_ context.Context, row pgx.Row) (*crypto.MerchantWebhookEndpoint, error) {
	var endpoint crypto.MerchantWebhookEndpoint
	var enabled []string
	err := row.Scan(&endpoint.ID, &endpoint.MerchantID, &endpoint.Name, &endpoint.URL,
		&enabled, &endpoint.IsDefault, &endpoint.IsActive, &endpoint.SecretRef,
		&endpoint.SecretVersion, &endpoint.PreviousSecretRef, &endpoint.PreviousSecretVersion,
		&endpoint.PreviousSecretValidUntil, &endpoint.Version, &endpoint.CreatedAt, &endpoint.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("webhook endpoint", "")
		}
		return nil, fmt.Errorf("scan webhook endpoint: %w", err)
	}
	endpoint.EnabledEvents = make([]crypto.WebhookEventType, len(enabled))
	for i := range enabled {
		endpoint.EnabledEvents[i] = crypto.WebhookEventType(enabled[i])
	}
	return &endpoint, nil
}

func webhookEventStrings(events []crypto.WebhookEventType) []string {
	result := make([]string, len(events))
	for i := range events {
		result[i] = string(events[i])
	}
	return result
}

var _ crypto.MerchantWebhookRepository = (*MerchantWebhookRepo)(nil)
