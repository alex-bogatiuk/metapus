//go:build integration

package catalog_repo_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/organization"
	"metapus/internal/infrastructure/numerator"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/testsupport"
)

// An organization written without a code gets one from its series in the
// write's transaction: a rollback returns the number, concurrent writes take
// distinct numbers, and a code entered by hand is skipped by the series.
func TestOrganizationCodesComeFromTheirSeriesInTheWriteTransaction(t *testing.T) {
	tenant := testsupport.NewSecurityTenants(t)[0]
	ctx := tenant.Context(appctx.UserContext{UserID: id.New().String(), IsAdmin: true, Roles: []string{"admin"}})
	currencyID := id.New()
	_, err := tenant.Pool.Exec(ctx, `INSERT INTO cat_currencies(id, code, name, iso_code, symbol) VALUES($1, 'RUB', 'Рубль', 'RUB', '₽')`, currencyID)
	require.NoError(t, err)
	svc := organization.NewService(catalog_repo.NewOrganizationRepo(), numerator.New())
	create := func(ctx context.Context, code string) (*organization.Organization, error) {
		org := organization.NewOrganization(code, "Организация "+id.New().String()[:8], currencyID)
		return org, svc.Create(ctx, org)
	}

	first, err := create(ctx, "")
	require.NoError(t, err)
	require.Equal(t, "ORG-000001", first.Code)
	stored, err := svc.GetByID(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, "ORG-000001", stored.Code)

	rollback := errors.New("rollback fixture")
	require.ErrorIs(t, tenant.Tx.RunInTransaction(ctx, func(ctx context.Context) error {
		org, err := create(ctx, "")
		require.NoError(t, err)
		require.Equal(t, "ORG-000002", org.Code)
		return rollback
	}), rollback)
	second, err := create(ctx, "")
	require.NoError(t, err)
	require.Equal(t, "ORG-000002", second.Code, "a rolled-back write returns its number to the series")

	manual, err := create(ctx, "ORG-000003")
	require.NoError(t, err)
	require.Equal(t, "ORG-000003", manual.Code)
	third, err := create(ctx, "")
	require.NoError(t, err)
	require.Equal(t, "ORG-000004", third.Code, "a series code taken by hand is skipped")

	_, err = create(ctx, "ORG-000003")
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok && appErr.Code == apperror.CodeConflict, "%v", err)
	require.Equal(t, "code", appErr.Details["field"])

	const writers = 12
	codes := make([]string, writers)
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			org, err := create(ctx, "")
			if err != nil {
				errs <- err
				return
			}
			codes[i] = org.Code
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	sort.Strings(codes)
	for i, code := range codes {
		require.Equal(t, fmt.Sprintf("ORG-%06d", 5+i), code, "concurrent writes take consecutive distinct codes")
	}
}
