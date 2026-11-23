//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/notifications"
)

func TestNotificationBatchAndNotifiedUsers(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := NewTxManagerFromRawPool(pool)
	ctx := tenant.WithTxManager(context.Background(), txm)

	reached, deleted, other := id.New(), id.New(), id.New()
	for _, user := range []id.ID{reached, deleted, other} {
		_, err := pool.Exec(ctx, `INSERT INTO auth_users(id,email,password_hash) VALUES($1,$2,'x')`, user, "notified-"+id.Compact(user)+"@example.test")
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM auth_users WHERE id = ANY($1)`, []id.ID{reached, deleted, other})
	})

	repo := NewNotificationRepo()
	event := "evt-" + id.Compact(id.New())
	link := "/portal/settings"
	items := []*notifications.Notification{
		{UserID: reached, Title: "t", Message: "m", Severity: notifications.SeverityWarning, Link: &link, Attributes: map[string]any{"eventId": event, "count": 2}},
		{UserID: deleted, Title: "t", Message: "m", Attributes: map[string]any{"eventId": event}},
		{UserID: other, Title: "t", Message: "m", Attributes: map[string]any{"eventId": "another"}},
	}
	require.NoError(t, txm.RunInTransaction(ctx, func(ctx context.Context) error { return repo.CreateBatch(ctx, items) }))
	require.NoError(t, repo.CreateBatch(ctx, nil))

	var severity, storedLink string
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT severity,link,(attributes->>'count')::int FROM sys_notifications WHERE id=$1`, *items[0].ID).Scan(&severity, &storedLink, &count))
	require.Equal(t, "warning", severity)
	require.Equal(t, link, storedLink)
	require.Equal(t, 2, count)
	require.NoError(t, pool.QueryRow(ctx, `SELECT severity FROM sys_notifications WHERE id=$1`, *items[1].ID).Scan(&severity))
	require.Equal(t, "info", severity, "an unset severity is info")

	_, err = pool.Exec(ctx, `UPDATE sys_notifications SET is_read=TRUE,deletion_mark=TRUE WHERE id=$1`, *items[1].ID)
	require.NoError(t, err)
	users, err := repo.UsersNotifiedWith(ctx, "eventId", event)
	require.NoError(t, err)
	require.ElementsMatch(t, []id.ID{reached, deleted}, users, "a deleted notification still counts as delivered")
	users, err = repo.UsersNotifiedWith(ctx, "eventId", "missing")
	require.NoError(t, err)
	require.Empty(t, users)

	broken := []*notifications.Notification{
		{UserID: reached, Title: "t", Message: "m", Attributes: map[string]any{"eventId": "batch-" + event}},
		{UserID: id.New(), Title: "t", Message: "m", Attributes: map[string]any{"eventId": "batch-" + event}},
	}
	require.Error(t, txm.RunInTransaction(ctx, func(ctx context.Context) error { return repo.CreateBatch(ctx, broken) }))
	users, err = repo.UsersNotifiedWith(ctx, "eventId", "batch-"+event)
	require.NoError(t, err)
	require.Empty(t, users, "a batch with an unknown user writes nothing")
}
