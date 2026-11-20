package integrations

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/providerpolicy"
)

func sendAt(g *poolGovernor, count int, now time.Time) {
	for range count {
		g.record(now)
	}
}

func TestGovernorHalvesMeasuredRateOncePerBurst(t *testing.T) {
	g := &poolGovernor{reported: map[id.ID]time.Time{}}
	now := time.Now()
	sendAt(g, 20, now) // 10 requests per second over the window.
	account := id.New()

	require.False(t, g.Throttled(account, now.Add(time.Second), now))
	require.InDelta(t, 5, g.limit, 0.01)
	require.False(t, g.Throttled(account, now.Add(time.Second), now.Add(100*time.Millisecond)), "the same account is not a shared limit")
	require.InDelta(t, 5, g.limit, 0.01, "429s of one burst cut the rate once")

	later := now.Add(1500 * time.Millisecond)
	require.False(t, g.Throttled(account, later.Add(time.Second), later))
	// The limit grew by 1.4/s since the last refusal: 6.4 is below the measured
	// 10/s, so the cut starts from the limit.
	require.InDelta(t, 3.2, g.limit, 0.01, "a cut never starts above the current limit")
}

func TestGovernorDetectsLimitSharedAcrossAccounts(t *testing.T) {
	g := &poolGovernor{reported: map[id.ID]time.Time{}}
	now := time.Now()
	first, second := id.New(), id.New()
	require.False(t, g.Throttled(first, now.Add(time.Second), now))
	retry := now.Add(3 * time.Second)
	require.True(t, g.Throttled(second, retry, now.Add(200*time.Millisecond)))
	require.Equal(t, retry, g.pausedUntil)

	g = &poolGovernor{reported: map[id.ID]time.Time{}}
	require.False(t, g.Throttled(first, now.Add(time.Second), now))
	require.False(t, g.Throttled(second, now.Add(time.Second), now.Add(governorSharedWindow)), "429s far apart are separate account limits")
}

func TestGovernorLeavesLongThrottlesToAccountRotation(t *testing.T) {
	g := &poolGovernor{reported: map[id.ID]time.Time{}}
	now := time.Now()
	require.False(t, g.Throttled(id.New(), now.Add(time.Hour), now))
	require.False(t, g.Throttled(id.New(), now.Add(time.Hour), now))
	require.Zero(t, g.limit)
	require.True(t, g.pausedUntil.IsZero())
}

func TestGovernorRecoversLinearlyAndReleasesLimit(t *testing.T) {
	g := &poolGovernor{reported: map[id.ID]time.Time{}}
	now := time.Now()
	sendAt(g, 8, now)
	g.Throttled(id.New(), now.Add(time.Second), now)
	require.InDelta(t, 2, g.limit, 0.01)

	g.recover(now.Add(500 * time.Millisecond))
	require.InDelta(t, 2, g.limit, 0.01, "no growth while the provider still refuses")
	g.recover(now.Add(4 * time.Second))
	require.InDelta(t, 5.5, g.limit, 0.01, "growth of 1/s counted from the end of the refusal hold")
	g.recover(now.Add(governorReleaseAfter))
	require.Zero(t, g.limit, "a limit far above demand is lifted")
}

func TestGovernorWaitFailsFastWhenPauseOutlastsDeadline(t *testing.T) {
	g := &poolGovernor{reported: map[id.ID]time.Time{}, pausedUntil: time.Now().Add(time.Minute)}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := g.Wait(ctx, time.Now())
	require.ErrorIs(t, err, providerpolicy.ErrAdmissionBusy)
	require.Less(t, time.Since(started), 40*time.Millisecond)
}

func TestGovernorWaitPacesSendsAtLimit(t *testing.T) {
	g := &poolGovernor{reported: map[id.ID]time.Time{}, limit: 20}
	g.throttledAt, g.adjustedAt = time.Now(), time.Now()
	started := time.Now()
	for range 3 {
		require.NoError(t, g.Wait(t.Context(), time.Now()))
	}
	require.GreaterOrEqual(t, time.Since(started), 90*time.Millisecond, "three sends at 20/s span two intervals")
}

func TestGovernorReportsOneThrottlePerAccountWindow(t *testing.T) {
	g := &poolGovernor{reported: map[id.ID]time.Time{}}
	now := time.Now()
	account := id.New()
	require.True(t, g.ShouldReport(account, now.Add(5*time.Second), now))
	require.False(t, g.ShouldReport(account, now.Add(6*time.Second), now.Add(time.Second)))
	require.True(t, g.ShouldReport(id.New(), now.Add(5*time.Second), now))
	require.True(t, g.ShouldReport(account, now.Add(10*time.Second), now.Add(5*time.Second)), "a new window reports again")
}

func threeAccounts() *memoryAccounts {
	repo := fixture()
	repo.selections = append(repo.selections, model.Selection{PoolID: repo.selections[0].PoolID, AccountID: id.New(), CredentialsVersion: 1, Slot: "default", Key: "key-c"})
	return repo
}

func TestTransportStopsRotatingWhenAccountsShareRateLimit(t *testing.T) {
	repo := threeAccounts()
	var keys []string
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		keys = append(keys, r.Header.Get("x-cg-demo-api-key"))
		resp := response(r, http.StatusTooManyRequests, `{"error":"too many requests"}`)
		resp.Header.Set("Retry-After", "3")
		return resp, nil
	})
	tr := &Transport{Base: base, Repository: repo, PoolID: repo.selections[0].PoolID, Provider: model.CoinGeckoDemo}
	req, err := http.NewRequest(http.MethodGet, "https://api.coingecko.com/test", nil)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	require.Equal(t, []string{"key-a", "key-b"}, keys, "the third account is not tried against a shared limit")
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.Equal(t, "3", resp.Header.Get("Retry-After"))
	require.True(t, strings.Contains(string(body), "rate limited"))
	require.Equal(t, []model.Failure{model.Throttled}, repo.reports, "only the first account is marked throttled")
}

func TestTransportRotatesPastAccountLevelRateLimit(t *testing.T) {
	repo := threeAccounts()
	var keys []string
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		keys = append(keys, r.Header.Get("x-cg-demo-api-key"))
		if len(keys) == 1 {
			return response(r, http.StatusTooManyRequests, `{"error":"too many requests"}`), nil
		}
		return response(r, http.StatusOK, `{"result":"ok"}`), nil
	})
	tr := &Transport{Base: base, Repository: repo, PoolID: repo.selections[0].PoolID, Provider: model.CoinGeckoDemo}
	req, err := http.NewRequest(http.MethodGet, "https://api.coingecko.com/test", nil)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, []string{"key-a", "key-b"}, keys)
	require.Equal(t, []model.Failure{model.Throttled}, repo.reports)
	require.Positive(t, governorFor(t.Context(), repo.selections[0].PoolID).limit, "the next account is paced below the refused rate")
}

func TestGovernorLetsUrgentSendOvertakeWaitingBulk(t *testing.T) {
	g := &poolGovernor{reported: map[id.ID]time.Time{}, limit: 10}
	g.throttledAt, g.adjustedAt = time.Now(), time.Now()
	bulk := providerpolicy.WithBulk(t.Context(), true)
	require.NoError(t, g.Wait(bulk, time.Now())) // Takes the free slot.

	done := make(chan string, 2)
	go func() {
		if g.Wait(bulk, time.Now()) == nil {
			done <- "bulk"
		}
	}()
	time.Sleep(20 * time.Millisecond) // The bulk send is now waiting for a slot.
	go func() {
		if g.Wait(t.Context(), time.Now()) == nil {
			done <- "urgent"
		}
	}()
	require.Equal(t, "urgent", <-done, "an urgent send takes the next slot ahead of waiting bulk")
	require.Equal(t, "bulk", <-done)
}

func TestGovernorKeepsBulkWithinItsShareOfLimit(t *testing.T) {
	g := &poolGovernor{reported: map[id.ID]time.Time{}, limit: 20}
	g.throttledAt, g.adjustedAt = time.Now(), time.Now()
	bulk := providerpolicy.WithBulk(t.Context(), true)
	started := time.Now()
	for range 5 {
		require.NoError(t, g.Wait(bulk, time.Now()))
	}
	// Four gaps at 20/s × 0.75 are 4 × 66.7 ms; urgent pacing would take 200 ms.
	require.GreaterOrEqual(t, time.Since(started), 250*time.Millisecond)
}

func TestGovernorBulkFailsFastWhenPauseOutlastsDeadline(t *testing.T) {
	g := &poolGovernor{reported: map[id.ID]time.Time{}, pausedUntil: time.Now().Add(time.Minute)}
	ctx, cancel := context.WithTimeout(providerpolicy.WithBulk(t.Context(), true), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	require.ErrorIs(t, g.Wait(ctx, time.Now()), providerpolicy.ErrAdmissionBusy)
	require.Less(t, time.Since(started), 40*time.Millisecond)
}
