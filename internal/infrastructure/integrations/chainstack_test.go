package integrations

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/rpcpolicy"
)

func TestChainstackRotatesMoreThanFourAccountsAndPreservesRequest(t *testing.T) {
	pid := id.New()
	repo := &memoryAccounts{}
	for range 6 {
		repo.selections = append(repo.selections, model.Selection{PoolID: pid, AccountID: id.New(), Slot: "nile", Key: id.Compact(id.New())})
	}
	var paths []string
	tr := &Transport{PoolID: pid, Provider: model.Chainstack, Slot: "nile", Repository: repo, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, `{"num":10}`, string(body))
		if len(paths) < 6 {
			return response(r, 429, `{"error":"monthly request quota exhausted"}`), nil
		}
		return response(r, 200, `[]`), nil
	})}
	t.Cleanup(tr.CloseIdleConnections)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://tron-nile.core.chainstack.com/walletsolidity/gettransactioninfobyblocknum", strings.NewReader(`{"num":10}`))
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Len(t, paths, 6)
	require.Equal(t, "/"+repo.selections[5].Key+"/walletsolidity/gettransactioninfobyblocknum", paths[5])
	require.Equal(t, "/walletsolidity/gettransactioninfobyblocknum", req.URL.Path)
	require.Len(t, repo.reports, 5)
}

func TestChainstackSharesAccountPacerAcrossBindingsAndHonorsCancellation(t *testing.T) {
	repo := fixture()
	makeTransport := func() *Transport {
		return &Transport{PoolID: repo.selections[0].PoolID, Provider: model.Chainstack, Repository: repo, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(r, 200, `{}`), nil })}
	}
	first, second := makeTransport(), makeTransport()
	t.Cleanup(first.CloseIdleConnections)
	t.Cleanup(second.CloseIdleConnections)
	read := func(ctx context.Context, tr *Transport) error {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://tron-nile.core.chainstack.com/wallet/getnowblock", nil)
		resp, err := tr.RoundTrip(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		return err
	}
	require.NoError(t, read(t.Context(), first))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.Error(t, read(ctx, second), "a second binding must not obtain a new burst token")
	started := time.Now()
	require.NoError(t, read(t.Context(), second))
	require.GreaterOrEqual(t, time.Since(started), 150*time.Millisecond)
}

type unavailableAccounts struct{ err error }

func (r unavailableAccounts) Select(context.Context, id.ID, string, model.Provider) (model.Selection, error) {
	return model.Selection{}, r.err
}
func (unavailableAccounts) Report(context.Context, model.Selection, model.Failure, time.Time) error {
	return nil
}

func TestChainstackPreservesPoolCauseAndRejectsForeignHosts(t *testing.T) {
	want := &model.PoolUnavailableError{Reason: model.Exhausted, RetryAt: time.Now().Add(time.Hour)}
	tr := &Transport{PoolID: id.New(), Provider: model.Chainstack, Repository: unavailableAccounts{want}}
	req, _ := http.NewRequest(http.MethodPost, "https://tron-nile.core.chainstack.com/wallet/getnowblock", nil)
	_, err := tr.RoundTrip(req)
	var got *model.PoolUnavailableError
	require.ErrorAs(t, err, &got)
	require.Equal(t, want, got)
	tr.Repository = unavailableAccounts{errors.New("private-database-error")}
	_, err = tr.RoundTrip(req)
	require.NotContains(t, err.Error(), "private-database-error")
	repo := fixture()
	tr.Repository = repo
	req, _ = http.NewRequest(http.MethodPost, "https://example.com/wallet/getnowblock", nil)
	_, err = tr.RoundTrip(req)
	require.ErrorContains(t, err, "invalid Chainstack")
}

func TestChainstackRetryHintAndAmbiguousForbidden(t *testing.T) {
	now := time.Now()
	at := providerRetryAt(model.Chainstack, "", []byte(`{"error":{"code":-32005,"data":{"try_again_in":"292ms"}}}`), now)
	require.False(t, at.Before(now.Add(292*time.Millisecond)))
	require.WithinDuration(t, now.Add(292*time.Millisecond), at, 250*time.Millisecond)
	at = providerRetryAt(model.Chainstack, "1", []byte(`{}`), now)
	require.False(t, at.Before(now.Add(time.Second)))
	require.WithinDuration(t, now.Add(time.Second), at, 250*time.Millisecond)
	require.Equal(t, model.Throttled, Classify(model.Chainstack, 429, []byte(`{"error":{"code":-32005,"message":"RPS limit exceeded"}}`)))
	require.Empty(t, Classify(model.Chainstack, 403, []byte(`{"error":"access forbidden"}`)))
	require.Equal(t, model.Exhausted, Classify(model.Chainstack, 403, []byte(`{"error":"monthly quota exhausted"}`)))
}

func TestChainstackSepoliaRotatesAccountsAndPreservesJSONRPC(t *testing.T) {
	repo := fixture()
	body := `{"jsonrpc":"2.0","id":73,"method":"eth_getBlockByNumber","params":["0x64",true]}`
	var paths []string
	tr := &Transport{PoolID: repo.selections[0].PoolID, Provider: model.Chainstack, Repository: repo, OriginalURL: "https://ethereum-sepolia.core.chainstack.com/", Target: func(context.Context) (Binding, error) {
		return Binding{URL: "https://ethereum-sepolia.core.chainstack.com", PoolID: repo.selections[0].PoolID, Provider: model.Chainstack, Slot: "default"}, nil
	}, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(raw))
		require.Empty(t, r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("TRON-PRO-API-KEY"))
		paths = append(paths, r.URL.Path)
		if len(paths) == 1 {
			return response(r, 200, `{"error":{"code":-32005,"message":"monthly request quota exhausted"}}`), nil
		}
		return response(r, 200, `{"jsonrpc":"2.0","id":73,"result":{}}`), nil
	})}
	t.Cleanup(tr.CloseIdleConnections)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, tr.OriginalURL, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "unused")
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, []string{"/key-a", "/key-b"}, paths)
	require.Equal(t, []model.Failure{model.Exhausted}, repo.reports)
	require.Equal(t, "/", req.URL.Path)
}

func TestChainstackRejectsCrossProtocolRequestPaths(t *testing.T) {
	for _, raw := range []string{"https://ethereum-sepolia.core.chainstack.com/wallet/getnowblock", "https://ethereum-sepolia.core.chainstack.com/beacon", "https://ethereum-sepolia.core.chainstack.com/?token=x", "https://tron-nile.core.chainstack.com/", "https://ethereum-sepolia.core.chainstack.com/%2f"} {
		t.Run(raw, func(t *testing.T) {
			repo := fixture()
			tr := &Transport{PoolID: repo.selections[0].PoolID, Provider: model.Chainstack, Repository: repo, Base: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("invalid request reached wire"); return nil, nil })}
			req, err := http.NewRequest(http.MethodPost, raw, nil)
			require.NoError(t, err)
			_, err = tr.RoundTrip(req)
			require.ErrorContains(t, err, "invalid Chainstack")
		})
	}
}

func TestChainstackArchiveForbiddenDoesNotRotateOrLeakResponse(t *testing.T) {
	repo := fixture()
	tr := &Transport{PoolID: repo.selections[0].PoolID, Provider: model.Chainstack, Repository: repo, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, 403, `{"error":{"code":-32002,"message":"Archive, Debug and Trace requests are not available on your current plan. Visit https://provider.invalid/private-token to upgrade.."}}`), nil
	})}
	t.Cleanup(tr.CloseIdleConnections)
	req, err := http.NewRequest(http.MethodPost, "https://ethereum-sepolia.core.chainstack.com", strings.NewReader(`{"method":"eth_getBlockByNumber"}`))
	require.NoError(t, err)
	_, err = tr.RoundTrip(req)
	require.ErrorIs(t, err, rpcpolicy.ErrArchiveUnavailable)
	require.NotContains(t, err.Error(), "private-token")
	require.Empty(t, repo.reports)
	require.Equal(t, 0, repo.current)
}
