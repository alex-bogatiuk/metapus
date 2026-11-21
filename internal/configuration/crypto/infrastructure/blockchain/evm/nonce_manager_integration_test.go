//go:build integration

package evm

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/migration"
)

func TestNonceReservationIsConcurrentIdempotentAndReclaimsOnlyUnsignedTail(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}
	_, err := migration.RunAll(dsn)
	require.NoError(t, err)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManagerFromRawPool(pool))

	networkID := id.MustParse("11155111-0000-4000-8000-000000000001")
	// Every stress iteration owns a fresh valid address. Nonce lease events are
	// append-only production evidence and must never be deleted for test cleanup.
	source := "0x" + id.Compact(id.New()) + "00000000"

	manager := NewPostgresNonceManager()
	const count = 24
	basisIDs := make([]id.ID, count)
	results := make([]uint64, count)
	errorsByIndex := make([]error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		basisIDs[i] = id.New()
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			lease, reserveErr := manager.Reserve(ctx, NonceRequest{
				NetworkID: networkID, SourceAddress: source, BasisType: "CryptoSweepAttempt",
				BasisID: basisIDs[index], Purpose: crypto.SigningPurposeSweep, Attempt: 1, Generation: 1,
			})
			results[index], errorsByIndex[index] = lease.Nonce, reserveErr
		}(i)
	}
	wg.Wait()
	for _, reserveErr := range errorsByIndex {
		require.NoError(t, reserveErr)
	}
	sorted := append([]uint64(nil), results...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for i := range sorted {
		require.Equal(t, uint64(i), sorted[i])
	}

	tailIndex := 0
	for i, nonce := range results {
		if nonce == count-1 {
			tailIndex = i
			break
		}
	}
	repeated, err := manager.Reserve(ctx, NonceRequest{NetworkID: networkID, SourceAddress: source,
		BasisType: "CryptoSweepAttempt", BasisID: basisIDs[tailIndex], Purpose: crypto.SigningPurposeSweep, Attempt: 1, Generation: 1})
	require.NoError(t, err)
	require.Equal(t, uint64(count-1), repeated.Nonce)

	tailRequest := NonceRequest{NetworkID: networkID, SourceAddress: source, BasisType: "CryptoSweepAttempt",
		BasisID: basisIDs[tailIndex], Purpose: crypto.SigningPurposeSweep, Attempt: 1, Generation: 1}
	require.NoError(t, manager.ReclaimUnsignedTail(ctx, tailRequest))
	reusedTail, err := manager.Reserve(ctx, NonceRequest{NetworkID: networkID, SourceAddress: source,
		BasisType: "CryptoSweepAttempt", BasisID: id.New(), Purpose: crypto.SigningPurposeSweep, Attempt: 1, Generation: 1})
	require.NoError(t, err)
	require.Equal(t, uint64(count-1), reusedTail.Nonce)
}

type changingStartupObserver struct {
	change func()
	latest uint64
}

func (o changingStartupObserver) ObserveNonceState(context.Context, string) (ProviderNonceSnapshot, error) {
	if o.change != nil {
		o.change()
	}
	return ProviderNonceSnapshot{Latest: o.latest, Pending: o.latest, Observations: 2}, nil
}
func (o changingStartupObserver) AssessReplacement(context.Context, string, string, []string) (crypto.ReplacementAssessment, error) {
	return crypto.ReplacementAssessment{}, nil
}

func TestNonceStartupConcurrentReservationRetriesWithoutFalseDivergence(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	ctx := tenant.WithTxManager(t.Context(), postgres.NewTxManagerFromRawPool(pool))
	network, source := id.New(), "0x"+id.Compact(id.New())+"00000000"
	_, err = pool.Exec(ctx, "INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES($1,$1::uuid::text,'Nonce concurrency fixture','evm',$1::uuid::text,'ETH',18)", network)
	require.NoError(t, err)
	manager := NewPostgresNonceManager()
	reserve := func() {
		_, err := manager.Reserve(ctx, NonceRequest{NetworkID: network, SourceAddress: source, BasisType: "CryptoSweepAttempt", BasisID: id.New(), Purpose: crypto.SigningPurposeSweep, Attempt: 1, Generation: 1})
		require.NoError(t, err)
	}
	reserve()
	divergences, err := manager.ReconcileStartup(ctx, network, changingStartupObserver{change: reserve})
	require.ErrorContains(t, err, "state changed")
	require.Empty(t, divergences, "a newer DB cursor is concurrency, not a security incident")
	var next int
	require.NoError(t, pool.QueryRow(ctx, "SELECT next_nonce FROM reg_evm_nonce_cursors WHERE network_id=$1 AND LOWER(source_address)=LOWER($2)", network, source).Scan(&next))
	require.Equal(t, 2, next)
	divergences, err = manager.ReconcileStartup(ctx, network, changingStartupObserver{})
	require.NoError(t, err)
	require.Empty(t, divergences)
	divergences, err = manager.ReconcileStartup(ctx, network, changingStartupObserver{latest: 3})
	require.NoError(t, err)
	require.Len(t, divergences, 1, "real chain advancement beyond the current cursor still fails closed")
}

// A provider-disagreement hold is reported as verified only when the startup
// checks pass again, and released only for the exact verified cursor.
func TestNonceStartupVerifiesAndReleasesOnlyProviderDisagreementHolds(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	defer pool.Close()
	ctx := tenant.WithTxManager(t.Context(), postgres.NewTxManagerFromRawPool(pool))
	network := id.New()
	_, err = pool.Exec(ctx, "INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals) VALUES($1,$1::uuid::text,'Nonce hold release fixture','evm',$1::uuid::text,'ETH',18)", network)
	require.NoError(t, err)
	manager := NewPostgresNonceManager()
	disagreed, consumed := "0x"+id.Compact(id.New())+"00000000", "0x"+id.Compact(id.New())+"00000000"
	for _, source := range []string{disagreed, consumed} {
		_, err := manager.Reserve(ctx, NonceRequest{NetworkID: network, SourceAddress: source, BasisType: "CryptoSweepAttempt", BasisID: id.New(), Purpose: crypto.SigningPurposeSweep, Attempt: 1, Generation: 1})
		require.NoError(t, err)
		_, err = pool.Exec(ctx, "UPDATE reg_evm_nonce_leases SET status='finalized' WHERE network_id=$1 AND LOWER(source_address)=LOWER($2)", network, source)
		require.NoError(t, err)
	}
	require.NoError(t, manager.HoldSource(ctx, network, disagreed, NonceProviderDisagreement))
	require.NoError(t, manager.HoldSource(ctx, network, consumed, "nonce 0 was consumed without a persisted transaction hash"))

	report, err := manager.ReconcileStartupReport(ctx, network, changingStartupObserver{latest: 1})
	require.NoError(t, err)
	require.Empty(t, report.Divergences)
	require.Len(t, report.Verified, 1, "only the provider-disagreement hold is release-eligible")
	verified := report.Verified[0]
	require.Equal(t, "1", verified.NextNonce)

	stale := verified
	stale.NextNonce = "2"
	require.NoError(t, manager.ReleaseVerifiedHold(ctx, network, stale))
	held := func(source string) bool {
		var value bool
		require.NoError(t, pool.QueryRow(ctx, "SELECT held FROM reg_evm_nonce_cursors WHERE network_id=$1 AND LOWER(source_address)=LOWER($2)", network, source).Scan(&value))
		return value
	}
	require.True(t, held(disagreed), "a cursor that moved after verification stays held")
	require.NoError(t, manager.ReleaseVerifiedHold(ctx, network, verified))
	require.False(t, held(disagreed))
	require.True(t, held(consumed), "chain or DB evidence holds stay for an operator")

	report, err = manager.ReconcileStartupReport(ctx, network, changingStartupObserver{latest: 1})
	require.NoError(t, err)
	require.Empty(t, report.Verified, "a released cursor is not reported again")
}
