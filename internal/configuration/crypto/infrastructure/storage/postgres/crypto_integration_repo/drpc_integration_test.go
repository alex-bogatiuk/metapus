//go:build integration

package crypto_integration_repo

import (
	"context"
	"errors"
	"fmt"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/integration_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
	"github.com/stretchr/testify/require"

	"sync"
	"testing"
	"time"
)

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
	a, err := NewIntegrationAccountRepo().GetByCode(ctx, importedCode("ACC", "drpc/drpc-import"))
	require.NoError(t, err)
	require.EqualValues(t, 1, a.ManualPeriodVersion)
	next := end.Add(time.Hour)
	binding.CredentialsExpiresAt = &next
	bindings[target] = binding
	_, err = ImportConfiguration(ctx, cfg, bindings, false)
	require.ErrorContains(t, err, "renew explicitly")
	after, err := NewIntegrationAccountRepo().GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, a.CredentialsExpiresAt, after.CredentialsExpiresAt)
	require.Equal(t, a.CredentialsEnc, after.CredentialsEnc)
}

func TestDRPCConcurrentRenewalsAndReportsAcrossPools(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	ctx, _ := fixtureDB(t)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ar, pr := NewIntegrationAccountRepo(), NewIntegrationPoolRepo()
	a := integrations.NewAccount("DRPC-RACE", "Concurrent dRPC")
	a.Provider = cryptointegrations.DRPC
	end := time.Now().Add(time.Hour)
	a.CredentialsExpiresAt = &end
	a.PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "sepolia", Value: "original-key"}}}
	require.NoError(t, ar.Prepare(ctx, a, appDefinition(a.Provider)))
	require.NoError(t, ar.Create(ctx, a))
	r := integration_repo.NewRuntime(cryptointegrations.Catalog())
	var pools []*integrations.Pool
	var old []integrations.Selection
	for _, code := range []string{"DRPC-P", "DRPC-Q"} {
		p := integrations.NewPool(code, code)
		p.Provider = cryptointegrations.DRPC
		p.AccountIDs = append(p.AccountIDs, a.ID)
		require.NoError(t, pr.Create(ctx, p))
		pools = append(pools, p)
		s, err := r.Select(ctx, p.ID, "sepolia", cryptointegrations.DRPC)
		require.NoError(t, err)
		old = append(old, s)
	}
	candidates := make([]*integrations.Account, 2)
	for i := range candidates {
		var err error
		candidates[i], err = ar.GetByID(ctx, a.ID)
		require.NoError(t, err)
		next := end.Add(24 * time.Hour)
		candidates[i].CredentialsExpiresAt = &next
		candidates[i].PendingCredentials = &integrations.Credentials{Keys: []integrations.NamedKey{{Name: "sepolia", Value: fmt.Sprintf("renewed-%d", i)}}}
	}
	start := make(chan struct{})
	renewalErrors := make(chan error, 2)
	otherErrors := make(chan error, 20)
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		wg.Add(1)
		go func(a *integrations.Account) {
			defer wg.Done()
			<-start
			err := tenant.MustGetTxManager(ctx).RunInTransaction(ctx, func(tx context.Context) error {
				if err := ar.Prepare(tx, a, appDefinition(a.Provider)); err != nil {
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
			for _, f := range []integrations.Failure{integrations.Exhausted, integrations.Unauthorized, ""} {
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
			_, err := r.Select(ctx, pools[i%2].ID, "sepolia", cryptointegrations.DRPC)
			var unavailable *integrations.PoolUnavailableError
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
		s, err := r.Select(ctx, p.ID, "sepolia", cryptointegrations.DRPC)
		require.NoError(t, err)
		require.EqualValues(t, 2, s.ManualPeriodVersion)
		require.NotEqual(t, "original-key", s.Key)
		require.NoError(t, r.Report(ctx, s, "", time.Time{}))
		status, err := r.Status(ctx, p.ID)
		require.NoError(t, err)
		require.Equal(t, "available", status.Accounts[0].Availability)
	}
}
