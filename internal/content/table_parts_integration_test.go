//go:build integration

package content

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/crypto_sweep"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/storage/postgres"
)

// TestDeclaredTablePartsMatchSchema is the schema gate for physical table
// parts: every declared part has its table, a mandatory document_id with a
// single-column FK to the header id, and no other table uses the separator.
func TestDeclaredTablePartsMatchSchema(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn, "a migrated disposable database is required")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	factories := v1.NewFactoryRegistry()
	RegisterDefaults(factories)
	registry := v1.BuildReadRegistry(factories)
	declared := map[string]bool{}
	for _, def := range registry.List() {
		for _, tp := range def.TableParts {
			if tp.SQLTable == "" {
				continue
			}
			declared[tp.SQLTable] = true
			require.Equal(t, def.TableName+entity.PartSeparator, tp.SQLTable[:len(def.TableName)+len(entity.PartSeparator)], tp.SQLTable)
			var exists bool
			require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
				WHERE n.nspname='public' AND c.relkind IN ('r','p') AND c.relname=$1)`, tp.SQLTable).Scan(&exists))
			require.True(t, exists, "table part %s.%s has no table %s", def.Name, tp.Name, tp.SQLTable)
			var udt, nullable string
			require.NoError(t, pool.QueryRow(ctx, `SELECT udt_name,is_nullable FROM information_schema.columns
				WHERE table_schema='public' AND table_name=$1 AND column_name='document_id'`, tp.SQLTable).Scan(&udt, &nullable), tp.SQLTable)
			require.Equal(t, "uuid", udt, tp.SQLTable)
			require.Equal(t, "NO", nullable, tp.SQLTable)
			var fk bool
			require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(
				SELECT 1 FROM pg_constraint k
				JOIN pg_attribute src ON src.attrelid=k.conrelid AND src.attnum=k.conkey[1]
				JOIN pg_attribute dst ON dst.attrelid=k.confrelid AND dst.attnum=k.confkey[1]
				WHERE k.contype='f' AND k.conrelid=to_regclass('public.'||$1) AND k.confrelid=to_regclass('public.'||$2)
				  AND cardinality(k.conkey)=1 AND src.attname='document_id' AND dst.attname='id')`, tp.SQLTable, def.TableName).Scan(&fk))
			require.True(t, fk, "%s.document_id must reference %s(id)", tp.SQLTable, def.TableName)
		}
	}
	require.True(t, declared[crypto_sweep.Definition.MustPartTable(crypto_sweep.LinesPart)])

	rows, err := pool.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname='public' AND c.relkind IN ('r','p','v','m') AND strpos(c.relname,$1)>0`, entity.PartSeparator)
	require.NoError(t, err)
	var separated []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			require.NoError(t, err)
		}
		separated = append(separated, name)
	}
	rows.Close()
	require.NoError(t, rows.Err())
	for _, name := range separated {
		require.True(t, declared[name], "table %s uses the reserved separator without a declared part", name)
	}

	// Reference search reads the part table from the declaration and fails
	// instead of skipping a table it cannot query.
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManagerFromRawPool(pool))
	finder := postgres.NewRefFinderRepo(registry)
	for _, def := range registry.List() {
		_, err := finder.CountReferences(ctx, domain.FindReferencesRequest{EntityName: def.Name, EntityID: id.New()})
		require.NoError(t, err, def.Name)
		_, err = finder.CountReferencesBatch(ctx, def.Name, []id.ID{id.New()})
		require.NoError(t, err, def.Name)
	}
	rollback := errors.New("rollback fixture")
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		// Bypass unrelated FKs and lifecycle triggers: only the line's wallet reference matters.
		if _, err := q.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return err
		}
		sweepID, walletID := id.New(), id.New()
		if _, err := q.Exec(ctx, `INSERT INTO doc_crypto_sweeps(id,token_id,hot_wallet_id,total_amount,status) VALUES($1,$2,$3,1,'broadcast')`, sweepID, id.New(), id.New()); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `INSERT INTO doc_crypto_sweeps__lines(line_id,document_id,line_no,wallet_id,amount) VALUES($1,$2,1,$3,1),($4,$2,2,$3,1)`, id.New(), sweepID, walletID, id.New()); err != nil {
			return err
		}
		total, err := finder.CountReferences(ctx, domain.FindReferencesRequest{EntityName: wallet.Definition.Name, EntityID: walletID})
		require.NoError(t, err)
		require.Equal(t, 1, total, "two lines of one sweep are one referencing document")
		batch, err := finder.CountReferencesBatch(ctx, wallet.Definition.Name, []id.ID{walletID})
		require.NoError(t, err)
		require.Equal(t, 1, batch[walletID])
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
