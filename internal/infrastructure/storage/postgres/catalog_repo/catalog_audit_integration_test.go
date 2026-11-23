//go:build integration

package catalog_repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/counterparty"
	"github.com/alex-bogatiuk/metapus/internal/domain/eventlog"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/testsupport"
)

type auditFailure struct {
	eventlog.Writer
	fail bool
}

func (w *auditFailure) Write(ctx context.Context, event eventlog.Event) error {
	if w.fail {
		return errors.New("injected audit writer failure")
	}
	return w.Writer.Write(ctx, event)
}

func TestCatalogAuditAndOutboxCommitAtomically(t *testing.T) {
	tenant := testsupport.NewSecurityTenants(t)[0]
	principal := appctx.UserContext{UserID: id.New().String()}
	ctx := tenant.Context(principal)
	_, err := tenant.Pool.Exec(ctx, `INSERT INTO auth_users(id,email,password_hash) VALUES($1,'audit@test','fixture')`, principal.UserID)
	require.NoError(t, err)
	repo := catalog_repo.NewCounterpartyRepo()
	svc := counterparty.NewService(repo, nil)
	writer := &auditFailure{Writer: postgres.NewEventLogRepo()}
	domain.NewEventLogCatalogService(svc.CatalogService, "counterparty", writer)
	outbox := postgres.NewOutboxPublisher()
	svc.Hooks().On(domain.TransactionCreate, func(ctx context.Context, row *counterparty.Counterparty) error {
		return outbox.Publish(ctx, domain.DomainEvent{AggregateType: "counterparty", AggregateID: row.ID, EventType: "CatalogCreated", Payload: map[string]any{"id": row.ID, "version": row.Version}})
	})
	var callbacks []string
	svc.Hooks().OnAfterCreate(func(ctx context.Context, row *counterparty.Counterparty) error {
		require.Nil(t, tenant.Tx.GetTx(ctx), "committed callbacks must not reuse a closed tx")
		var committedName string
		require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT name FROM cat_counterparties WHERE id=$1", row.ID).Scan(&committedName))
		callbacks = append(callbacks, row.Name)
		return nil
	})
	newRow := func(code string) *counterparty.Counterparty {
		row := counterparty.NewCounterparty(code, code, counterparty.TypeCustomer, counterparty.LegalCompany)
		row.Attributes["credential"] = "never-audit-this-secret"
		return row
	}
	count := func(table string, uid id.ID) int {
		t.Helper()
		column := "entity_id"
		if table == "cat_counterparties" {
			column = "id"
		}
		if table == "sys_outbox" {
			column = "aggregate_id"
		}
		var n int
		require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+column+"=$1", uid).Scan(&n))
		return n
	}
	rolledBack := newRow("ROLLBACK")
	err = tenant.Tx.RunInTransaction(ctx, func(txCtx context.Context) error {
		require.NoError(t, svc.Create(txCtx, rolledBack))
		require.Empty(t, callbacks)
		for _, table := range []string{"cat_counterparties", "sys_event_log", "sys_outbox"} {
			require.Zero(t, count(table, rolledBack.ID))
		}
		return errors.New("presenter rejected the response")
	})
	require.Error(t, err)
	for _, table := range []string{"cat_counterparties", "sys_event_log", "sys_outbox"} {
		require.Zero(t, count(table, rolledBack.ID))
	}
	require.Empty(t, callbacks)
	committed := newRow("COMMITTED")
	require.NoError(t, tenant.Tx.RunInTransaction(ctx, func(txCtx context.Context) error {
		require.NoError(t, svc.Create(txCtx, committed))
		committed.Name = "caller changed the object"
		require.Empty(t, callbacks)
		return nil
	}))
	require.Equal(t, []string{"COMMITTED"}, callbacks)
	for _, table := range []string{"cat_counterparties", "sys_event_log", "sys_outbox"} {
		require.Equal(t, 1, count(table, committed.ID))
	}
	for _, operation := range []string{"create", "update", "mark", "delete"} {
		t.Run("writer failure/"+operation, func(t *testing.T) {
			row, err := repo.GetByID(ctx, committed.ID)
			require.NoError(t, err)
			writer.fail = true
			defer func() { writer.fail = false }()
			failed := newRow("FAIL-" + operation)
			require.NoError(t, tenant.Tx.RunInTransaction(ctx, func(txCtx context.Context) error {
				var err error
				switch operation {
				case "create":
					err = svc.Create(txCtx, failed)
				case "update":
					row.Name = "should roll back"
					err = svc.Update(txCtx, row)
				case "mark":
					err = svc.SetDeletionMark(txCtx, row.ID, true)
				case "delete":
					err = svc.Delete(txCtx, row.ID)
				}
				require.ErrorContains(t, err, "injected audit")
				return nil // A caller catching the error must not commit the failed mutation.
			}))
			require.Zero(t, count("cat_counterparties", failed.ID))
			actual, err := repo.GetByID(ctx, committed.ID)
			require.NoError(t, err)
			require.Equal(t, "COMMITTED", actual.Name)
			require.Equal(t, 1, actual.Version)
			require.False(t, actual.DeletionMark)
			require.Equal(t, 1, count("sys_event_log", committed.ID))
		})
	}
	row, err := repo.GetByID(ctx, committed.ID)
	require.NoError(t, err)
	row.Name = "Updated"
	require.NoError(t, svc.Update(ctx, row))
	require.NoError(t, svc.SetDeletionMark(ctx, row.ID, true))
	require.NoError(t, svc.SetDeletionMark(ctx, row.ID, false))
	require.NoError(t, svc.Delete(ctx, row.ID))
	rows, err := tenant.Pool.Query(ctx, `SELECT event_type, user_id::text, details FROM sys_event_log WHERE entity_id=$1 ORDER BY created_at,id`, row.ID)
	require.NoError(t, err)
	defer rows.Close()
	var events []string
	var versions []int
	for rows.Next() {
		var kind, actor string
		var payload []byte
		require.NoError(t, rows.Scan(&kind, &actor, &payload))
		require.Equal(t, principal.UserID, actor)
		require.NotContains(t, string(payload), "never-audit-this-secret")
		var detail struct {
			Operation string `json:"operation"`
			Version   int    `json:"version"`
		}
		require.NoError(t, json.Unmarshal(payload, &detail))
		require.Equal(t, kind, detail.Operation)
		events = append(events, kind)
		versions = append(versions, detail.Version)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"catalog.create", "catalog.update", "catalog.set_deletion_mark", "catalog.clear_deletion_mark", "catalog.delete"}, events)
	require.Equal(t, []int{1, 2, 3, 4, 4}, versions)
}
