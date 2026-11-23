//go:build integration

package crypto_repo

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

func TestConcurrentWebhookSecretRotationsAreSerializedAndRetired(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	merchantID := id.New()
	endpointID := id.New()
	suffix := id.Compact(merchantID)
	_, err = pool.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Webhook rotation test')`, merchantID, "WEBHOOK-ROTATE-"+suffix)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_webhook_secret_retirements WHERE secret_ref LIKE $1`, "merchant-webhooks/"+merchantID.String()+"/%")
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_merchant_webhook_endpoints WHERE merchant_id=$1`, merchantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_merchants WHERE id=$1`, merchantID)
	})

	repo := NewMerchantWebhookRepo()
	initialRef := "merchant-webhooks/" + merchantID.String() + "/" + endpointID.String() + "/v1"
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return repo.CreateEndpoint(txCtx, &crypto.MerchantWebhookEndpoint{
			ID: endpointID, MerchantID: merchantID, Name: "Primary", URL: "https://1.1.1.1/webhook",
			EnabledEvents: []crypto.WebhookEventType{crypto.WebhookInvoiceCompleted}, IsDefault: true,
			IsActive: true, SecretRef: initialRef, SecretVersion: 1,
		})
	})
	require.NoError(t, err)

	rotationRefs := []string{
		"merchant-webhooks/" + merchantID.String() + "/" + endpointID.String() + "/rot_a",
		"merchant-webhooks/" + merchantID.String() + "/" + endpointID.String() + "/rot_b",
	}
	versions := make(chan int, len(rotationRefs))
	errors := make(chan error, len(rotationRefs))
	var wait sync.WaitGroup
	for _, secretRef := range rotationRefs {
		wait.Add(1)
		go func(ref string) {
			defer wait.Done()
			var version int
			rotateErr := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
				rotated, err := repo.RotateEndpointSecret(txCtx, merchantID, endpointID, ref, time.Now().UTC())
				if err == nil {
					version = rotated.SecretVersion
				}
				return err
			})
			if rotateErr != nil {
				errors <- rotateErr
				return
			}
			versions <- version
		}(secretRef)
	}
	wait.Wait()
	close(errors)
	close(versions)
	for rotateErr := range errors {
		require.NoError(t, rotateErr)
	}
	seenVersions := map[int]bool{}
	for version := range versions {
		seenVersions[version] = true
	}
	require.Equal(t, map[int]bool{2: true, 3: true}, seenVersions)

	endpoint, err := repo.GetEndpoint(ctx, merchantID, endpointID)
	require.NoError(t, err)
	require.Equal(t, 3, endpoint.SecretVersion)
	require.Contains(t, rotationRefs, endpoint.SecretRef)
	require.Contains(t, rotationRefs, endpoint.PreviousSecretRef)
	require.NotEqual(t, endpoint.SecretRef, endpoint.PreviousSecretRef)

	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return repo.DeleteEndpoint(txCtx, merchantID, endpointID)
	})
	require.NoError(t, err)

	allRefs := append([]string{initialRef}, rotationRefs...)
	var retirementCount int
	var earliest time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*),MIN(retire_after)
		FROM sys_webhook_secret_retirements WHERE secret_ref=ANY($1)
	`, allRefs).Scan(&retirementCount, &earliest))
	require.Equal(t, 3, retirementCount)
	require.True(t, earliest.After(time.Now().UTC().Add(72*time.Hour)))
}

func TestManualWebhookDeliveryJobUsesManualScheduleStart(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	merchantID, endpointID, aggregateID := id.New(), id.New(), id.New()
	eventID := "evt_" + id.Compact(id.New())
	now := time.Now().UTC().Truncate(time.Microsecond)
	repo := NewMerchantWebhookRepo()
	require.NoError(t, txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		if _, err := q.Exec(txCtx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Manual webhook schedule test')`,
			merchantID, "WEBHOOK-MANUAL-"+id.Compact(merchantID)); err != nil {
			return err
		}
		if err := repo.CreateEndpoint(txCtx, &crypto.MerchantWebhookEndpoint{
			ID: endpointID, MerchantID: merchantID, Name: "Manual target", URL: "https://1.1.1.1/webhook",
			EnabledEvents: []crypto.WebhookEventType{crypto.WebhookInvoiceCompleted},
			IsActive:      true, SecretRef: "merchant-webhooks/manual/" + endpointID.String(), SecretVersion: 1,
		}); err != nil {
			return err
		}
		if _, err := q.Exec(txCtx, `
			INSERT INTO sys_merchant_events(
				id,event_id,merchant_id,aggregate_type,aggregate_id,event_type,api_version,
				resource_version,causation_key,payload,created_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$9::jsonb,$10)
		`, id.New(), eventID, merchantID, crypto.MerchantWebhookAggregateType, aggregateID,
			crypto.WebhookInvoiceCompleted, crypto.MerchantWebhookAPIVersion, eventID, `{}`, now.Add(-20*24*time.Hour)); err != nil {
			return err
		}
		conflictErr := repo.CreateManualJob(txCtx, merchantID, eventID, endpointID, now)
		require.ErrorContains(t, conflictErr, "terminal automatic delivery failure")
		if _, err := q.Exec(txCtx, `
			INSERT INTO sys_webhook_delivery_jobs(
				id,event_id,endpoint_id,merchant_id,snapshot_url,secret_ref,secret_version,
				next_attempt_at,deadline_at,status,attempt_count,manual,created_at,updated_at)
			VALUES($1,$2,$3,$4,'https://1.1.1.1/webhook',$5,1,$6,$7,'failed',10,FALSE,$6,$6)
		`, id.New(), eventID, endpointID, merchantID,
			"merchant-webhooks/manual/"+endpointID.String(), now.Add(-time.Hour), now); err != nil {
			return err
		}
		return repo.CreateManualJob(txCtx, merchantID, eventID, endpointID, now)
	}))
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_outbox WHERE aggregate_type=$1 AND aggregate_id=$2`, crypto.MerchantWebhookAggregateType, aggregateID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_webhook_delivery_jobs WHERE event_id=$1`, eventID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sys_merchant_events WHERE event_id=$1`, eventID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_merchant_webhook_endpoints WHERE id=$1`, endpointID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_merchants WHERE id=$1`, merchantID)
	})

	var jobs []crypto.WebhookDeliveryJob
	var unfinished bool
	require.NoError(t, txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var listErr error
		jobs, unfinished, listErr = repo.ListDueJobs(txCtx, eventID, now.Add(time.Second))
		return listErr
	}))
	require.True(t, unfinished)
	require.Len(t, jobs, 1)
	require.True(t, jobs[0].Manual)
	require.WithinDuration(t, now, jobs[0].ScheduleStartedAt, time.Millisecond)
	require.WithinDuration(t, now.Add(72*time.Hour), jobs[0].DeadlineAt, time.Millisecond)
}
