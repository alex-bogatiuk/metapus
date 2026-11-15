package dashboardapp

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/domain/dashboard"
	er "metapus/internal/domain/registers/exchange_rate"
	"testing"
	"time"
)

type fakeRepo struct {
	assets []dashboard.Asset
	calls  int
	err    error
}

func (r *fakeRepo) Queues(context.Context, string, int) (dashboard.Queues, error) {
	r.calls++
	return dashboard.Queues{Total: 61}, r.err
}
func (r *fakeRepo) Assets(context.Context) ([]dashboard.Asset, error) {
	r.calls++
	return r.assets, r.err
}
func (r *fakeRepo) Analytics(context.Context, string) (dashboard.Analytics, error) {
	r.calls++
	return dashboard.Analytics{}, r.err
}

type fakeRates struct{}

func (fakeRates) LoadConfiguration(context.Context) (er.Configuration, error) {
	return er.Configuration{BaseCurrency: er.CurrencyRef{ID: id.New(), Code: "USD", DecimalPlaces: 2}}, nil
}
func (fakeRates) Candidates(context.Context, er.Configuration, []id.ID, time.Time) ([]er.Candidate, error) {
	return nil, nil
}
func TestInvalidFiltersNeverReachRepository(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil, func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	_, err := svc.Queues(context.Background(), "bogus", 5)
	require.Error(t, err)
	for _, limit := range []int{0, 21, -1} {
		_, err = svc.Queues(context.Background(), "all", limit)
		require.Error(t, err)
	}
	_, err = svc.Analytics(context.Background(), "year")
	require.Error(t, err)
	require.Zero(t, repo.calls)
	_, err = svc.Queues(context.Background(), "all", 20)
	require.NoError(t, err)
	require.Equal(t, 1, repo.calls)
}
func TestFinanceKeepsExactNativeBalancesWhenQuotesAreMissing(t *testing.T) {
	token, network := id.New(), id.New()
	repo := &fakeRepo{assets: []dashboard.Asset{{TokenID: token.String(), NetworkID: network.String(), Symbol: "TKN", DecimalPlaces: 18, MerchantMinor: "900719925474099312345678901", PayoutPendingMinor: "99", ObligationMinor: "900719925474099312345679000", WalletMinor: "1000000000000000001"}}}
	snapshots := 0
	svc := NewService(repo, er.NewService(fakeRates{}), func(ctx context.Context, fn func(context.Context) error) error { snapshots++; return fn(ctx) })
	finance, err := svc.Finance(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, snapshots)
	// Money in an unfinished payout is still owed, so it is valued with the available balance.
	require.Equal(t, "900719925474099312345679000", finance.Obligations.ByToken[0].RawAmount)
	require.Equal(t, "1000000000000000001", finance.Wallets.ByToken[0].RawAmount)
	require.Nil(t, finance.Obligations.Valuation.Total.Amount)
	require.Equal(t, "none", finance.Obligations.Valuation.Total.Coverage)
	require.NotZero(t, finance.ObservedAt)
}
func TestReadFailurePropagatesInsteadOfBecomingZero(t *testing.T) {
	failure := errors.New("read failed")
	repo := &fakeRepo{err: failure}
	svc := NewService(repo, nil, func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	_, err := svc.Queues(context.Background(), "all", 5)
	require.ErrorIs(t, err, failure)
	_, err = svc.Analytics(context.Background(), "30d")
	require.ErrorIs(t, err, failure)
}
