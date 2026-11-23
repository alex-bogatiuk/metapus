//go:build integration

package portal_repo

import (
	"context"
	"net/netip"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

func TestConcurrentMerchantCIDRPolicyUpdateNeverLetsDeniedRequestFailOpen(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	merchantID, actorID := id.New(), id.New()
	suffix := id.Compact(merchantID)
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_merchants(id,code,name)
		VALUES($1,$2,'CIDR concurrency integration merchant')
	`, merchantID, "CIDR-RACE-"+suffix)
	require.NoError(t, err)

	repo := NewMerchantNetworkPolicyRepo()
	service := cryptoapp.NewMerchantNetworkPolicyService(txm, repo)
	managementIP := netip.MustParseAddr("198.51.100.42")
	deniedIP := netip.MustParseAddr("203.0.113.10")
	_, err = service.AddNetwork(ctx, merchantID, merchant.NetworkSurfacePortal, nil,
		"198.51.100.0/24", "operations", actorID, "prepare integration policy", false)
	require.NoError(t, err)
	_, err = service.SetMode(ctx, merchantID, merchant.NetworkSurfacePortal, nil,
		merchant.NetworkPolicyWarn, actorID, managementIP, "stage warn policy", false)
	require.NoError(t, err)

	const readers = 64
	start := make(chan struct{})
	decisions := make(chan merchant.NetworkAccessDecision, readers)
	errorsFound := make(chan error, readers+1)
	var wait sync.WaitGroup

	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		_, updateErr := service.SetMode(ctx, merchantID, merchant.NetworkSurfacePortal, nil,
			merchant.NetworkPolicyEnforce, actorID, managementIP, "enforce after staged validation", false)
		if updateErr != nil {
			errorsFound <- updateErr
		}
	}()

	for range readers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			requestErr := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
				policies, loadErr := repo.GetEffectiveNetworkPolicies(txCtx, merchantID, merchant.NetworkSurfacePortal, nil)
				if loadErr != nil {
					return loadErr
				}
				decision := merchant.EvaluateNetworkPolicies(policies, deniedIP)
				if decision == merchant.NetworkAccessAllow {
					decisions <- decision
					return nil
				}
				var policyID *id.ID
				if len(policies) > 0 && !id.IsNil(policies[0].ID) {
					policyID = &policies[0].ID
				}
				if auditErr := repo.RecordNetworkDecision(txCtx, merchantID, policyID,
					"request", "integration-request", decision, deniedIP); auditErr != nil {
					return auditErr
				}
				decisions <- decision
				return nil
			})
			if requestErr != nil {
				errorsFound <- requestErr
			}
		}()
	}
	close(start)
	wait.Wait()
	close(decisions)
	close(errorsFound)

	for concurrentErr := range errorsFound {
		require.NoError(t, concurrentErr)
	}
	decisionCount := 0
	for decision := range decisions {
		decisionCount++
		require.Contains(t, []merchant.NetworkAccessDecision{
			merchant.NetworkAccessWarn,
			merchant.NetworkAccessDeny,
		}, decision)
	}
	require.Equal(t, readers, decisionCount)

	policies, err := repo.GetEffectiveNetworkPolicies(ctx, merchantID, merchant.NetworkSurfacePortal, nil)
	require.NoError(t, err)
	require.Equal(t, merchant.NetworkAccessDeny, merchant.EvaluateNetworkPolicies(policies, deniedIP))
	var audited int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM auth_merchant_network_policy_audit
		WHERE merchant_id=$1 AND action='request_evaluated' AND client_ip=$2::inet
	`, merchantID, deniedIP.String()).Scan(&audited))
	require.Equal(t, readers, audited)
}
