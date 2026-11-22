//go:build integration

package crypto_worker

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/tenant"

	"metapus/pkg/sdk/workerjob"
)

func TestWorkerReliabilityWebhookClaimsAcrossReplicas(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_DATABASE_DSN is required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	ctx = tenant.WithTxManager(ctx, sdktest.NewTxManager(pool))
	merchants := []id.ID{id.New(), id.New()}
	defer func() {
		cleanup := context.Background()
		_, _ = pool.Exec(cleanup, `DELETE FROM sys_webhook_delivery_jobs WHERE merchant_id=ANY($1)`, merchants)
		_, _ = pool.Exec(cleanup, `DELETE FROM sys_merchant_events WHERE merchant_id=ANY($1)`, merchants)
		_, _ = pool.Exec(cleanup, `DELETE FROM cat_merchants WHERE id=ANY($1)`, merchants)
	}()
	for i, merchantID := range merchants {
		_, err := pool.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Webhook ownership')`, merchantID, id.Compact(merchantID))
		require.NoError(t, err)
		for range 2 - i {
			eventID := "evt_" + id.Compact(id.New())
			_, err = pool.Exec(ctx, `INSERT INTO sys_merchant_events(id,event_id,merchant_id,aggregate_type,aggregate_id,event_type,api_version,resource_version,causation_key,payload,created_at)
 VALUES($1,$2,$3,$4,$1,'invoice.paid','test',1,$2,'{}',clock_timestamp())`, id.New(), eventID, merchantID, crypto.MerchantWebhookAggregateType)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO sys_webhook_delivery_jobs(id,event_id,merchant_id,snapshot_url,secret_ref,secret_version,next_attempt_at,deadline_at)
 VALUES($1,$2,$3,'https://merchant.example/webhook','test',1,clock_timestamp()-INTERVAL '1 second',clock_timestamp()+INTERVAL '72 hours')`, id.New(), eventID, merchantID)
			require.NoError(t, err)
		}
	}
	repo := crypto_repo.NewMerchantWebhookRepo()
	jobsCh, errorsCh := make(chan []crypto.WebhookDeliveryJob, 8), make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			jobs, err := repo.ClaimDueJobs(ctx, time.Now().UTC(), 1)
			jobsCh <- jobs
			errorsCh <- err
		})
	}
	wg.Wait()
	close(jobsCh)
	close(errorsCh)
	for err := range errorsCh {
		require.NoError(t, err)
	}
	var jobs []crypto.WebhookDeliveryJob
	for result := range jobsCh {
		jobs = append(jobs, result...)
	}
	require.Len(t, jobs, 2)
	require.NotEqual(t, jobs[0].MerchantID, jobs[1].MerchantID)
	old := jobs[0]
	_, err = pool.Exec(ctx, `UPDATE sys_webhook_delivery_jobs SET lease_until=clock_timestamp()-INTERVAL '1 second' WHERE id=$1`, old.ID)
	require.NoError(t, err)
	require.NoError(t, RecoverMerchantWebhookWork(ctx))
	reclaimed, unfinished, err := repo.ListDueJobs(ctx, old.EventID, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, unfinished)
	require.Len(t, reclaimed, 1)
	require.Equal(t, old.ID, reclaimed[0].ID)
	require.NotEqual(t, old.ClaimToken, reclaimed[0].ClaimToken)
	require.ErrorIs(t, repo.RenewJob(ctx, old.ID, old.ClaimToken), workerjob.ErrLeaseLost)
	require.ErrorIs(t, repo.MarkJobSucceeded(ctx, old.ID, old.ClaimToken), workerjob.ErrLeaseLost)
	require.ErrorIs(t, repo.MarkJobRetry(ctx, old.ID, old.ClaimToken, 1, time.Now(), false, "stale"), workerjob.ErrLeaseLost)
	require.NoError(t, repo.MarkJobSucceeded(ctx, reclaimed[0].ID, reclaimed[0].ClaimToken))
	require.NoError(t, repo.MarkJobSucceeded(ctx, jobs[1].ID, jobs[1].ClaimToken))
}
