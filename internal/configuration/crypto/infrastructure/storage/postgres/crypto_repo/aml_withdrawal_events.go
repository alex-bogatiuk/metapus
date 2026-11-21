package crypto_repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/infrastructure/storage/postgres"
)

func (r *AMLRepository) PublishNextAMLWithdrawalEvent(ctx context.Context) (bool, error) {
	published := false
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var eventID, request, merchant id.ID
		var eventType crypto.WebhookEventType
		var version int
		var occurred time.Time
		var snapshot []byte
		err := q.QueryRow(ctx, `SELECT e.id,e.request_id,e.merchant_id,e.resource_version,e.event_type,e.created_at,e.snapshot
 FROM reg_crypto_aml_withdrawal_events e WHERE NOT EXISTS(SELECT 1 FROM sys_merchant_events me WHERE me.merchant_id=e.merchant_id AND me.causation_key='aml-withdrawal:'||e.id::text)
 ORDER BY e.created_at,e.id FOR UPDATE OF e SKIP LOCKED LIMIT 1`).Scan(&eventID, &request, &merchant, &version, &eventType, &occurred, &snapshot)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var data map[string]any
		if err := json.Unmarshal(snapshot, &data); err != nil {
			return err
		}
		if err := NewLifecycleWebhookOutbox().PublishLifecycleWebhook(ctx, crypto.LifecycleWebhookEvent{AggregateID: request, MerchantID: merchant, Event: eventType, ResourceVersion: version, CausationKey: "aml-withdrawal:" + eventID.String(), OccurredAt: occurred, Data: data}); err != nil {
			return err
		}
		published = true
		return nil
	})
	return published, err
}
