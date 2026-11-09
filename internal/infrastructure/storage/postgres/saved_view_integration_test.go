//go:build integration

package postgres

import (
	"context"
	"io/fs"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	dbassets "metapus/db"
	"metapus/internal/core/apperror"
	corectx "metapus/internal/core/context"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/automations"
	"metapus/internal/domain/listview"
	"metapus/internal/domain/reports/variants"
)

func markerTestDB(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_MARKERS_DSN")
	require.NotEmpty(t, dsn, "use an isolated disposable database")
	adminPool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(adminPool.Close)
	database := "markers_" + uuid.New().String()[:8]
	quotedDatabase := pgx.Identifier{database}.Sanitize()
	_, err = adminPool.Exec(t.Context(), "CREATE DATABASE "+quotedDatabase)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := adminPool.Exec(context.Background(), "DROP DATABASE "+quotedDatabase+" WITH (FORCE)")
		require.NoError(t, err)
	})
	config := adminPool.Config().Copy()
	config.ConnConfig.Database = database
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	sqlDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	source, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, source)
	require.NoError(t, err)
	_, err = provider.Up(t.Context())
	require.NoError(t, err, "the original migrations must bootstrap a clean database")
	return pool, tenant.WithTxManager(t.Context(), NewTxManagerFromRawPool(pool))
}

func markerAuthor(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	_, err := pool.Exec(t.Context(), `INSERT INTO auth_users(id,email,password_hash) VALUES($1,$2,'test')`, userID, userID.String()+"@example.test")
	require.NoError(t, err)
	return userID
}

func TestMarkerReportVariantCompetingWriters(t *testing.T) {
	pool, ctx := markerTestDB(t)
	owner := markerAuthor(t, pool)
	repo := NewReportVariantRepo()
	v := &variants.ReportVariant{ID: uuid.New(), DatasetKey: "marker-test", Name: "Original", AuthorID: &owner, Visibility: variants.VisibilityPersonal}
	require.NoError(t, repo.Create(ctx, v))
	first, stale := *v, *v
	first.Name = "First writer"
	require.NoError(t, repo.Update(ctx, &first))
	require.Equal(t, 2, first.Version)
	stale.Name = "Stale writer"
	require.True(t, apperror.IsConcurrentModification(repo.Update(ctx, &stale)))
	stored, err := repo.GetByID(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, first.Name, stored.Name)
	start, results := make(chan struct{}), make(chan error, 2)
	for _, name := range []string{"Concurrent A", "Concurrent B"} {
		candidate := *stored
		candidate.Name = name
		go func() { <-start; results <- repo.Update(ctx, &candidate) }()
	}
	close(start)
	errors := []error{<-results, <-results}
	if errors[0] == nil {
		require.True(t, apperror.IsConcurrentModification(errors[1]))
	} else {
		require.True(t, apperror.IsConcurrentModification(errors[0]))
		require.NoError(t, errors[1])
	}
}

func TestMarkerListViewDefaultsAndVersionConflictRollback(t *testing.T) {
	pool, ctx := markerTestDB(t)
	owner, other := markerAuthor(t, pool), markerAuthor(t, pool)
	ownerCtx := corectx.WithUser(ctx, &corectx.UserContext{UserID: owner.String()})
	otherCtx := corectx.WithUser(ctx, &corectx.UserContext{UserID: other.String()})
	repo, svc := NewListViewRepo(), listview.NewService(NewListViewRepo())
	view := func(name string, isDefault bool) *listview.ListView {
		return &listview.ListView{ID: uuid.New(), EntityType: "MarkerCounterparty", Name: name, Visibility: listview.VisibilityPersonal, IsDefault: isDefault}
	}
	previous, target, otherDefault := view("Previous", true), view("Target", false), view("Other user", true)
	require.NoError(t, svc.Create(ownerCtx, previous))
	require.NoError(t, svc.Create(ownerCtx, target))
	require.NoError(t, svc.Create(otherCtx, otherDefault))
	stale := *target
	require.NoError(t, repo.Update(ctx, target))
	stale.IsDefault = true
	require.True(t, apperror.IsConcurrentModification(svc.Update(ownerCtx, &stale)))
	get := func(id uuid.UUID) *listview.ListView {
		t.Helper()
		v, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		return v
	}
	require.True(t, get(previous.ID).IsDefault, "reset must roll back with the stale update")
	require.Equal(t, previous.Version, get(previous.ID).Version)
	require.NoError(t, svc.SetDefault(ownerCtx, target.ID))
	require.True(t, get(target.ID).IsDefault)
	require.False(t, get(previous.ID).IsDefault)
	require.Equal(t, previous.Version+1, get(previous.ID).Version)
	require.True(t, get(otherDefault.ID).IsDefault, "another user's default must not change")
	require.Error(t, svc.SetDefault(otherCtx, target.ID))
}

func TestMarkerAutomationStorageAndUnsupportedReactions(t *testing.T) {
	pool, ctx := markerTestDB(t)
	repo := NewAutomationRuleRepo()
	request := automations.CreateRuleRequest{Name: "Marker rule", TriggerType: automations.TriggerEntityEvent, EventType: "posted", ReactionType: automations.ReactionNotify, MessageFormat: "text", ActionTemplate: "First", IsActive: true}
	request.Normalize()
	require.NoError(t, request.Validate(ctx))
	rule, err := repo.Create(ctx, request)
	require.NoError(t, err)
	updated, err := repo.Update(ctx, rule.ID, automations.UpdateRuleRequest{Name: "Updated marker rule", TriggerType: request.TriggerType, EventType: request.EventType, ReactionType: request.ReactionType, MessageFormat: "text", NotifSeverity: "info", ActionTemplate: "Updated", IsActive: true, Version: rule.Version})
	require.NoError(t, err)
	require.Equal(t, "Updated", updated.ActionTemplate)
	require.Equal(t, rule.Version+1, updated.Version)
	for _, reaction := range []string{"chain", "create_record"} {
		_, err := pool.Exec(ctx, `UPDATE sys_automation_rules SET reaction_type=$1 WHERE id=$2`, reaction, rule.ID)
		require.Error(t, err, "schema must reject reactions with no executor")
	}
}
