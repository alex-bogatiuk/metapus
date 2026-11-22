//go:build integration

package crypto_repo_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/tenant"
)

func TestIngestionNotificationsReplayRecipientsAndAtomicRollback(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn, "disposable integration PostgreSQL required")
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(t.Context(), `CREATE TEMP TABLE auth_users(id uuid PRIMARY KEY,is_admin boolean,is_active boolean,deletion_mark boolean DEFAULT FALSE);
	 CREATE TEMP TABLE sys_notifications(id uuid PRIMARY KEY,user_id uuid REFERENCES auth_users(id),title text,message text,severity text,link text,is_read boolean,attributes jsonb,created_at timestamptz DEFAULT now(),deletion_mark boolean DEFAULT FALSE);`)
	require.NoError(t, err)
	admin1, admin2, disabled, merchant := id.New(), id.New(), id.New(), id.New()
	_, err = pool.Exec(t.Context(), `INSERT INTO auth_users VALUES($1,TRUE,TRUE,FALSE),($2,TRUE,TRUE,FALSE),($3,TRUE,FALSE,FALSE),($4,FALSE,TRUE,FALSE)`, admin1, admin2, disabled, merchant)
	require.NoError(t, err)
	ctx := tenant.WithTxManager(t.Context(), postgres.NewTxManagerFromRawPool(pool))
	lag, progressAge := int64(84), int64(4)
	event := crypto.IngestionAlertTransition{ID: id.New().String(), At: time.Now().UTC(), Condition: crypto.IngestionCondition{Scope: "network/SEPOLIA", NetworkCode: "SEPOLIA", Severity: "error", Reason: "rpc_coverage", LagSeconds: &lag, ProgressAgeSeconds: &progressAge}}
	errors := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { errors <- crypto_repo.DeliverIngestionAlert(ctx, event) })
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM sys_notifications`).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM sys_notifications WHERE attributes->>'lagSeconds'='84' AND attributes->>'progressAgeSeconds'='4'`).Scan(&count))
	require.Equal(t, 2, count, "measurements must survive delivery for every recipient")
	_, err = pool.Exec(ctx, `UPDATE sys_notifications SET is_read=TRUE,deletion_mark=TRUE`)
	require.NoError(t, err)
	require.NoError(t, crypto_repo.DeliverIngestionAlert(ctx, event))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM sys_notifications`).Scan(&count))
	require.Equal(t, 2, count)
	_, err = pool.Exec(ctx, `CREATE FUNCTION pg_temp.reject_ingestion_notification() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected commit failure'; END $$;
	 CREATE CONSTRAINT TRIGGER reject_ingestion_notification AFTER INSERT ON sys_notifications DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pg_temp.reject_ingestion_notification();`)
	require.NoError(t, err)
	event.ID = id.New().String()
	require.Error(t, crypto_repo.DeliverIngestionAlert(ctx, event))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM sys_notifications`).Scan(&count))
	require.Equal(t, 2, count, "failed COMMIT must leave no partial recipient batch")
	_, err = pool.Exec(ctx, `DROP TRIGGER reject_ingestion_notification ON sys_notifications`)
	require.NoError(t, err)
	event.Recovered = true
	require.NoError(t, crypto_repo.DeliverIngestionAlert(ctx, event))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM sys_notifications WHERE severity='success'`).Scan(&count))
	require.Equal(t, 2, count)
	_, err = pool.Exec(ctx, `UPDATE auth_users SET is_active=FALSE WHERE is_admin`)
	require.NoError(t, err)
	event.ID = id.New().String()
	require.ErrorContains(t, crypto_repo.DeliverIngestionAlert(ctx, event), "no active system administrator")
}
