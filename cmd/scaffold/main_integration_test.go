//go:build integration

package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/core/numerator"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
)

type migrationDocument struct{ entity.Document }

func (*migrationDocument) GetDocumentType() string { return "ProbeDocument" }

func TestGeneratedMigrationsRoundTripAndDocumentHooksRollback(t *testing.T) {
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := context.Background()
	db, err := sql.Open("pgx", tenant.Pool.Config().ConnConfig.ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	root := t.TempDir()
	for _, fixture := range []struct {
		name, kind string
		hierarchy  bool
	}{{"probe_catalog", "catalog", false}, {"probe_tree", "catalog", true}, {"probe_document", "document", false}} {
		args := []string{"--name", fixture.name, "--type", fixture.kind}
		if fixture.hierarchy {
			args = append(args, "--hierarchical")
		}
		require.NoError(t, run(args, root, io.Discard))
		provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(filepath.Join(root, "db", "configuration", fixture.name)), goose.WithAllowOutofOrder(true))
		require.NoError(t, err)
		_, err = provider.Up(ctx)
		require.NoError(t, err)
		_, err = provider.ApplyVersion(ctx, provider.ListSources()[0].Version, false)
		require.NoError(t, err)
		_, err = provider.Up(ctx)
		require.NoError(t, err)
	}
	_, err = tenant.Pool.Exec(ctx, "CREATE TABLE probe_hook_effects (id UUID PRIMARY KEY)")
	require.NoError(t, err)
	repo := document_repo.NewBaseDocumentRepo("doc_probe_documents", postgres.ExtractDBColumns[migrationDocument](), func() *migrationDocument { return &migrationDocument{} })
	svc := domain.NewBaseHeaderDocumentService(domain.BaseHeaderDocumentServiceConfig[*migrationDocument]{Repo: repo, TxManager: tenant.Tx, Numerator: numerator.Noop(), EntityName: "probe_document"})
	svc.GetHooks().OnBeforeCreate(func(ctx context.Context, doc *migrationDocument) error {
		_, err := tenant.Tx.GetQuerier(ctx).Exec(ctx, "INSERT INTO probe_hook_effects(id) VALUES($1)", doc.ID)
		return err
	})
	failure := errors.New("mandatory effect failed")
	fail := true
	svc.GetHooks().On(domain.TransactionCreate, func(context.Context, *migrationDocument) error {
		if fail {
			return failure
		}
		return nil
	})
	callbacks := 0
	svc.GetHooks().OnAfterCreate(func(ctx context.Context, doc *migrationDocument) error {
		require.Nil(t, tenant.Tx.GetTx(ctx))
		var committed bool
		require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM doc_probe_documents WHERE id=$1)", doc.ID).Scan(&committed))
		require.True(t, committed)
		callbacks++
		return nil
	})
	mutationCtx := tenant.Context(appctx.UserContext{})
	doc := &migrationDocument{Document: entity.NewDocument()}
	doc.Number = "PROBE-ROLLBACK"
	// A command may catch the error and commit its own outcome. The failed
	// document attempt and Before hook must still roll back to a savepoint.
	require.NoError(t, tenant.Tx.RunInTransaction(mutationCtx, func(ctx context.Context) error {
		require.ErrorIs(t, svc.Create(ctx, doc), failure)
		return nil
	}))
	for _, table := range []string{"doc_probe_documents", "probe_hook_effects"} {
		var count int
		require.NoError(t, tenant.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", doc.ID).Scan(&count))
		require.Zero(t, count)
	}
	require.Zero(t, callbacks)
	fail = false
	require.NoError(t, tenant.Tx.RunInTransaction(mutationCtx, func(ctx context.Context) error {
		require.NoError(t, svc.Create(ctx, doc))
		require.Zero(t, callbacks)
		return nil
	}))
	require.Equal(t, 1, callbacks)
}
