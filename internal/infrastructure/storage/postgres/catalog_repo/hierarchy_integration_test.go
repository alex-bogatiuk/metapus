//go:build integration

package catalog_repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/domain/catalogs/counterparty"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres/testsupport"
)

func TestHierarchyWritersSerializeMovesAndRejectExistingCycles(t *testing.T) {
	tenants := testsupport.NewSecurityTenants(t)
	principal := appctx.UserContext{UserID: id.New().String()}
	base := tenants[0].Context(principal)
	repo := catalog_repo.NewCounterpartyRepo()
	svc := counterparty.NewService(repo, nil)
	create := func(code string) *counterparty.Counterparty {
		t.Helper()
		row := counterparty.NewCounterparty(code, code, counterparty.TypeCustomer, counterparty.LegalCompany)
		row.IsFolder = true
		require.NoError(t, svc.Create(base, row))
		return row
	}
	for _, direct := range []bool{false, true} {
		name := "service"
		if direct {
			name = "repository-import"
		}
		t.Run(name, func(t *testing.T) {
			a, b := create(name+"-A"), create(name+"-B")
			a.SetParent(b.ID)
			b.SetParent(a.ID)
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, row := range []*counterparty.Counterparty{a, b} {
				go func() {
					<-start
					if direct {
						results <- repo.Update(base, row)
					} else {
						results <- svc.Update(base, row)
					}
				}()
			}
			close(start)
			first, second := <-results, <-results
			require.NotEqual(t, first == nil, second == nil, "exactly one competing move must commit: %v / %v", first, second)
			for _, err := range []error{first, second} {
				if err != nil {
					require.True(t, apperror.IsAppError(err))
					require.Contains(t, err.Error(), "cycle")
				}
			}
			_, err := repo.GetTree(base, nil)
			require.NoError(t, err)
		})
	}
	t.Run("branch depth and folder conversion", func(t *testing.T) {
		def := counterparty.Definition
		def.Hierarchy.MaxDepth = 2
		limited := catalog_repo.NewCatalogRepoForDefinition(def, postgres.ExtractDBColumns[counterparty.Counterparty](), func() *counterparty.Counterparty { return &counterparty.Counterparty{} })

		parent, child, target := create("DEPTH-P"), create("DEPTH-C"), create("DEPTH-T")
		child.SetParent(parent.ID)
		require.NoError(t, limited.Update(base, child))
		parent.SetParent(target.ID)
		require.Error(t, limited.Update(base, parent), "moving a whole branch must validate descendant depth")
		parent.ClearParent()
		parent.IsFolder = false
		require.Error(t, limited.Update(base, parent), "an occupied folder cannot become an item")
	})
	t.Run("rootless legacy cycles return a controlled error", func(t *testing.T) {
		a, b := create("LEGACY-A"), create("LEGACY-B")
		_, err := tenants[0].Pool.Exec(base, `UPDATE cat_counterparties SET parent_id=CASE WHEN id=$1 THEN $2::uuid ELSE $1::uuid END WHERE id IN ($1,$2)`, a.ID, b.ID)
		require.NoError(t, err)
		for _, read := range []func() error{
			func() error { _, err := repo.GetTree(base, nil); return err },
			func() error { _, err := repo.GetTree(base, &a.ID); return err },
			func() error { _, err := repo.GetPath(base, a.ID); return err },
		} {
			err := read()
			require.Error(t, err)
			require.True(t, apperror.IsAppError(err))
		}
	})
	t.Run("locking port requires an active transaction", func(t *testing.T) {
		var locker domain.HierarchyLocker = repo
		require.Error(t, locker.LockHierarchy(base))
		require.NoError(t, tenants[1].Tx.RunInTransaction(tenants[1].Context(principal), func(ctx context.Context) error { return locker.LockHierarchy(ctx) }))
	})
}
