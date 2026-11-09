//go:build integration

package integration_repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"io/fs"
	dbassets "metapus/db"
	"metapus/internal/core/apperror"
	"metapus/internal/core/tenant"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDRPCManualExpiryRenewalAndLateReports(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, db := fixtureDB(t)
	ar, pr := catalog_repo.NewIntegrationAccountRepo(), catalog_repo.NewIntegrationPoolRepo()
	a := model.NewAccount("DRPC", "Manual dRPC")
	a.Provider = model.DRPC
	end := time.Now().Add(time.Hour)
	a.CredentialsExpiresAt = &end
	a.PendingCredentials = &model.Credentials{Keys: []model.NamedKey{{Name: "sepolia", Value: "first-key"}}}
	require.NoError(t, a.Validate(ctx))
	require.NoError(t, ar.Prepare(ctx, a))
	require.NoError(t, ar.Create(ctx, a))
	p := model.NewPool("DRPC", "dRPC")
	p.Provider = model.DRPC
	p.AccountIDs = append(p.AccountIDs, a.ID)
	require.NoError(t, pr.Create(ctx, p))
	now := a.ManualPeriodStartedAt.Add(time.Second)
	r := NewRuntimeWithClock(now)
	first, err := r.Select(ctx, p.ID, "sepolia", model.DRPC)
	require.NoError(t, err)
	require.Equal(t, "first-key", first.Key)
	require.EqualValues(t, 1, first.ManualPeriodVersion)
	require.NoError(t, r.Report(ctx, first, model.Exhausted, time.Time{}))
	require.NoError(t, r.Report(ctx, first, "", time.Time{}), "late success cannot renew an exhausted manual period")
	_, err = r.Select(ctx, p.ID, "sepolia", model.DRPC)
	require.Error(t, err)
	var held *model.PoolUnavailableError
	require.ErrorAs(t, err, &held)
	require.Equal(t, model.Exhausted, held.Reason)
	require.True(t, held.RetryAt.IsZero(), "expiry is not a scheduled quota reset")
	status, err := r.Status(ctx, p.ID)
	require.NoError(t, err)
	require.Nil(t, status.Accounts[0].NextResetAt)
	require.Equal(t, "quota_exhausted", status.Accounts[0].Availability)
	require.Error(t, r.Command(ctx, p.ID, "resume", status.SelectionVersion))
	loaded, err := ar.GetByID(ctx, a.ID)
	require.NoError(t, err)
	nextEnd := loaded.CredentialsExpiresAt.Add(24 * time.Hour)
	loaded.CredentialsExpiresAt = &nextEnd
	require.Error(t, ar.Prepare(ctx, loaded), "date alone cannot extend access")
	loaded.PendingCredentials = &model.Credentials{Keys: []model.NamedKey{{Name: "sepolia", Value: "first-key"}}}
	require.Error(t, ar.Prepare(ctx, loaded), "same plaintext cannot renew")
	loaded.PendingCredentials = &model.Credentials{Keys: []model.NamedKey{{Name: "sepolia", Value: "second-key"}}}
	require.NoError(t, ar.Prepare(ctx, loaded))
	require.NoError(t, ar.Update(ctx, loaded))
	r.Now = func() time.Time { return loaded.ManualPeriodStartedAt.Add(time.Second) }
	second, err := r.Select(ctx, p.ID, "sepolia", model.DRPC)
	require.NoError(t, err)
	require.EqualValues(t, 2, second.ManualPeriodVersion)
	require.Equal(t, "second-key", second.Key)
	require.NoError(t, r.Report(ctx, first, model.Unauthorized, time.Time{}))
	require.NoError(t, r.Report(ctx, first, "", time.Time{}))
	require.NoError(t, r.Report(ctx, second, "", time.Time{}))
	var generation int64
	var exhausted *time.Time
	var slots []string
	require.NoError(t, db.QueryRow(ctx, `SELECT manual_period_version,exhausted_until,invalid_slots FROM sys_integration_account_state WHERE account_id=$1`, a.ID).Scan(&generation, &exhausted, &slots))
	require.EqualValues(t, 2, generation)
	require.Nil(t, exhausted)
	require.Empty(t, slots)
	r.Now = func() time.Time { return *loaded.CredentialsExpiresAt }
	_, err = r.Select(ctx, p.ID, "sepolia", model.DRPC)
	var unavailable *model.PoolUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, model.Expired, unavailable.Reason)
	status, err = r.Status(ctx, p.ID)
	require.NoError(t, err)
	require.Nil(t, status.AccountID)
	require.Equal(t, "credentials_expired", status.Accounts[0].Availability)
	require.Error(t, r.Command(ctx, p.ID, "resume", status.SelectionVersion))
	require.Error(t, r.Command(ctx, p.ID, "exhaust", status.SelectionVersion))
	require.NoError(t, r.Command(ctx, p.ID, "next", status.SelectionVersion))
	_, err = r.Select(ctx, p.ID, "sepolia", model.DRPC)
	require.Error(t, err, "next cannot bypass credential expiry")
	r.Now = func() time.Time { return loaded.CredentialsExpiresAt.AddDate(0, 1, 0) }
	_, err = r.Select(ctx, p.ID, "sepolia", model.DRPC)
	require.Error(t, err, "month boundary never revives manual credentials")
	// The common catalog transaction rolls credentials, period, and version back together.
	before, err := ar.GetByID(ctx, a.ID)
	require.NoError(t, err)
	sentinel := errors.New("force rollback")
	err = tenant.MustGetTxManager(ctx).RunInTransaction(ctx, func(tx context.Context) error {
		updated, e := ar.GetByID(tx, a.ID)
		if e != nil {
			return e
		}
		future := time.Now().Add(72 * time.Hour)
		updated.CredentialsExpiresAt = &future
		updated.PendingCredentials = &model.Credentials{Keys: []model.NamedKey{{Name: "sepolia", Value: "third-key"}}}
		if e = ar.Prepare(tx, updated); e != nil {
			return e
		}
		if e = ar.Update(tx, updated); e != nil {
			return e
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	after, err := ar.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, before.CredentialsEnc, after.CredentialsEnc)
	require.Equal(t, before.ManualPeriodVersion, after.ManualPeriodVersion)
	require.Equal(t, before.CredentialsExpiresAt, after.CredentialsExpiresAt)
}
func TestDRPCSchemaUpgradeIsAdditiveAndRejectsPartialShape(t *testing.T) {
	ctx, db := fixtureDB(t)
	_, err := db.Exec(ctx, `ALTER TABLE cat_integration_accounts DROP CONSTRAINT cat_integration_accounts_manual_period_check, DROP COLUMN credentials_expires_at,DROP COLUMN manual_period_started_at,DROP COLUMN manual_period_version;ALTER TABLE sys_integration_account_state DROP COLUMN manual_period_version`)
	require.NoError(t, err)
	needed, err := UpgradeSchema(ctx, false)
	require.NoError(t, err)
	require.True(t, needed)
	var count int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name='cat_integration_accounts' AND column_name='credentials_expires_at'`).Scan(&count))
	require.Zero(t, count)
	needed, err = UpgradeSchema(ctx, true)
	require.NoError(t, err)
	require.True(t, needed)
	needed, err = UpgradeSchema(ctx, true)
	require.NoError(t, err)
	require.False(t, needed)
	var definition string
	require.NoError(t, db.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='cat_integration_accounts_provider_check'`).Scan(&definition))
	require.Contains(t, definition, "drpc")
	require.Contains(t, definition, "getblock_net")
	require.Contains(t, definition, "testnet_aml")
	_, err = db.Exec(ctx, `ALTER TABLE cat_integration_accounts DROP COLUMN credentials_expires_at CASCADE`)
	require.NoError(t, err)
	_, err = UpgradeSchema(ctx, true)
	require.ErrorContains(t, err, "partial")
}

func TestDRPCSchemaUpgradePreservesSupportedMigrationVersions(t *testing.T) {
	for _, version := range []int64{60, 63, 74} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			ctx, db := fixtureDB(t)
			migrationDB, err := sql.Open("pgx", db.Config().ConnConfig.ConnString())
			require.NoError(t, err)
			t.Cleanup(func() { _ = migrationDB.Close() })
			migrations, err := fs.Sub(dbassets.Files, "migrations")
			require.NoError(t, err)
			provider, err := goose.NewProvider(goose.DialectPostgres, migrationDB, migrations)
			require.NoError(t, err)
			_, err = provider.DownTo(ctx, version)
			require.NoError(t, err)
			_, err = db.Exec(ctx, `ALTER TABLE cat_integration_accounts DROP CONSTRAINT cat_integration_accounts_manual_period_check,DROP COLUMN credentials_expires_at,DROP COLUMN manual_period_started_at,DROP COLUMN manual_period_version; ALTER TABLE sys_integration_account_state DROP COLUMN manual_period_version`)
			require.NoError(t, err)
			values := "'alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','telegram','smtp','resource_api'"
			if version >= 63 {
				values += ",'getblock_net'"
			}
			if version >= 74 {
				values += ",'testnet_aml'"
			}
			for _, table := range []string{"cat_integration_accounts", "cat_integration_pools"} {
				_, err = db.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s_provider_check,ADD CONSTRAINT %s_provider_check CHECK(provider IN (%s))", table, table, table, values))
				require.NoError(t, err)
			}
			needed, err := UpgradeSchema(ctx, false)
			require.NoError(t, err)
			require.True(t, needed)
			needed, err = UpgradeSchema(ctx, true)
			require.NoError(t, err)
			require.True(t, needed)
			needed, err = UpgradeSchema(ctx, true)
			require.NoError(t, err)
			require.False(t, needed)
			for _, table := range []string{"cat_integration_accounts", "cat_integration_pools"} {
				var definition string
				require.NoError(t, db.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1`, table+"_provider_check").Scan(&definition))
				require.Contains(t, definition, "'drpc'")
				require.Equal(t, version >= 63, strings.Contains(definition, "'getblock_net'"))
				require.Equal(t, version >= 74, strings.Contains(definition, "'testnet_aml'"))
			}
			current, err := provider.GetDBVersion(ctx)
			require.NoError(t, err)
			require.Equal(t, version, current)
		})
	}
}

func TestDRPCImportRequiresExpiryAndCannotRenewExistingPeriod(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, _ := fixtureDB(t)
	cfg := &chainconfig.File{Networks: map[string]chainconfig.Network{"ETHEREUM-SEPOLIA": {Roles: map[chainconfig.Role][]chainconfig.Endpoint{chainconfig.RoleRPC: {{ProviderID: "drpc-import-test", URL: "https://lb.drpc.live/sepolia/", APIKey: "import-test-key", TrustGroup: "drpc", Priority: 90}}}}}}
	target := "ETHEREUM-SEPOLIA/rpc/drpc-import-test"
	bindings := map[string]ImportBinding{target: {ExternalAccountRef: "drpc-import", Name: "dRPC import", PoolCode: "DRPC-IMPORT", Slot: "sepolia"}}
	_, err := ImportConfiguration(ctx, cfg, bindings, false)
	require.ErrorContains(t, err, "expiry")
	end := time.Now().Add(time.Hour) // Nanoseconds are normalized to the database precision.
	binding := bindings[target]
	binding.CredentialsExpiresAt = &end
	bindings[target] = binding
	report, err := ImportConfiguration(ctx, cfg, bindings, false)
	require.NoError(t, err)
	require.Equal(t, 1, report.Accounts)
	require.Equal(t, 1, report.Endpoints)
	report, err = ImportConfiguration(ctx, cfg, bindings, false)
	require.NoError(t, err)
	require.Zero(t, report.Accounts)
	require.Zero(t, report.Endpoints)
	a, err := catalog_repo.NewIntegrationAccountRepo().GetByCode(ctx, importedCode("ACC", "drpc/drpc-import"))
	require.NoError(t, err)
	require.EqualValues(t, 1, a.ManualPeriodVersion)
	next := end.Add(time.Hour)
	binding.CredentialsExpiresAt = &next
	bindings[target] = binding
	_, err = ImportConfiguration(ctx, cfg, bindings, false)
	require.ErrorContains(t, err, "renew explicitly")
	after, err := catalog_repo.NewIntegrationAccountRepo().GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, a.CredentialsExpiresAt, after.CredentialsExpiresAt)
	require.Equal(t, a.CredentialsEnc, after.CredentialsEnc)
}

func TestDRPCConcurrentRenewalsAndReportsAcrossPools(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, _ := fixtureDB(t)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ar, pr := catalog_repo.NewIntegrationAccountRepo(), catalog_repo.NewIntegrationPoolRepo()
	a := model.NewAccount("DRPC-RACE", "Concurrent dRPC")
	a.Provider = model.DRPC
	end := time.Now().Add(time.Hour)
	a.CredentialsExpiresAt = &end
	a.PendingCredentials = &model.Credentials{Keys: []model.NamedKey{{Name: "sepolia", Value: "original-key"}}}
	require.NoError(t, ar.Prepare(ctx, a))
	require.NoError(t, ar.Create(ctx, a))
	r := NewRuntime()
	var pools []*model.Pool
	var old []model.Selection
	for _, code := range []string{"DRPC-P", "DRPC-Q"} {
		p := model.NewPool(code, code)
		p.Provider = model.DRPC
		p.AccountIDs = append(p.AccountIDs, a.ID)
		require.NoError(t, pr.Create(ctx, p))
		pools = append(pools, p)
		s, err := r.Select(ctx, p.ID, "sepolia", model.DRPC)
		require.NoError(t, err)
		old = append(old, s)
	}
	candidates := make([]*model.Account, 2)
	for i := range candidates {
		var err error
		candidates[i], err = ar.GetByID(ctx, a.ID)
		require.NoError(t, err)
		next := end.Add(24 * time.Hour)
		candidates[i].CredentialsExpiresAt = &next
		candidates[i].PendingCredentials = &model.Credentials{Keys: []model.NamedKey{{Name: "sepolia", Value: fmt.Sprintf("renewed-%d", i)}}}
	}
	start := make(chan struct{})
	renewalErrors := make(chan error, 2)
	otherErrors := make(chan error, 20)
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		wg.Add(1)
		go func(a *model.Account) {
			defer wg.Done()
			<-start
			err := tenant.MustGetTxManager(ctx).RunInTransaction(ctx, func(tx context.Context) error {
				if err := ar.Prepare(tx, a); err != nil {
					return err
				}
				return ar.Update(tx, a)
			})
			renewalErrors <- err
		}(candidate)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for _, s := range old {
			for _, f := range []model.Failure{model.Exhausted, model.Unauthorized, ""} {
				if err := r.Report(ctx, s, f, time.Time{}); err != nil {
					otherErrors <- err
				}
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := range 8 {
			_, err := r.Select(ctx, pools[i%2].ID, "sepolia", model.DRPC)
			var unavailable *model.PoolUnavailableError
			if err != nil && !errors.As(err, &unavailable) {
				otherErrors <- err
			}
		}
	}()
	close(start)
	wg.Wait()
	close(renewalErrors)
	close(otherErrors)
	succeeded, conflicted := 0, 0
	for err := range renewalErrors {
		if err == nil {
			succeeded++
		} else {
			var appErr *apperror.AppError
			require.ErrorAs(t, err, &appErr)
			require.Contains(t, err.Error(), "CONCURRENT_MODIFICATION")
			conflicted++
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, conflicted)
	for err := range otherErrors {
		require.NoError(t, err)
	}
	updated, err := ar.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.ManualPeriodVersion)
	for _, p := range pools {
		s, err := r.Select(ctx, p.ID, "sepolia", model.DRPC)
		require.NoError(t, err)
		require.EqualValues(t, 2, s.ManualPeriodVersion)
		require.NotEqual(t, "original-key", s.Key)
		require.NoError(t, r.Report(ctx, s, "", time.Time{}))
		status, err := r.Status(ctx, p.ID)
		require.NoError(t, err)
		require.Equal(t, "available", status.Accounts[0].Availability)
	}
}
