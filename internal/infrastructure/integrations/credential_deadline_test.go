package integrations

import (
	"context"
	"github.com/stretchr/testify/require"
	"io"
	"metapus/internal/core/id"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/rpcpolicy"
	"net/http"
	"strings"
	"testing"
	"time"
)

type clockAccounts struct {
	choices        []model.Selection
	now            func() time.Time
	calls, reports int
}

func (r *clockAccounts) Select(context.Context, id.ID, string, model.Provider) (model.Selection, error) {
	r.calls++
	for _, s := range r.choices {
		if !selectionExpired(s, r.now()) {
			return s, nil
		}
	}
	return model.Selection{}, &model.PoolUnavailableError{Reason: model.Expired}
}
func (r *clockAccounts) Report(context.Context, model.Selection, model.Failure, time.Time) error {
	r.reports++
	return nil
}
func deadlineFixture(now time.Time) (model.Selection, model.Selection) {
	pid := id.New()
	endA, endB := now.Add(time.Hour), now.Add(2*time.Hour)
	return model.Selection{PoolID: pid, AccountID: id.New(), Slot: "sepolia", Key: "first", CredentialsExpiresAt: &endA, ManualPeriodVersion: 1}, model.Selection{PoolID: pid, AccountID: id.New(), Slot: "sepolia", Key: "second", CredentialsExpiresAt: &endB, ManualPeriodVersion: 1}
}
func TestCredentialCacheCannotOutliveExpiry(t *testing.T) {
	now := time.Now().UTC()
	a, b := deadlineFixture(now)
	repo := &clockAccounts{choices: []model.Selection{a, b}, now: func() time.Time { return now }}
	var paths []string
	tr := &Transport{Repository: repo, Provider: model.DRPC, PoolID: a.PoolID, Slot: "sepolia", Now: func() time.Time { return now }, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		return response(r, 200, `{"result":"ok"}`), nil
	})}
	defer tr.CloseIdleConnections()
	for range 2 {
		req, _ := http.NewRequest(http.MethodPost, "https://lb.drpc.live/sepolia/", strings.NewReader(`{"id":1}`))
		res, err := tr.RoundTrip(req)
		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
		require.Equal(t, "/sepolia/", req.URL.Path)
		now = *a.CredentialsExpiresAt
	}
	require.Equal(t, []string{"/sepolia/first", "/sepolia/second"}, paths)
	require.Equal(t, 2, repo.calls)
	require.Zero(t, repo.reports)
}
func TestCredentialExpiryIsRecheckedAfterAdmission(t *testing.T) {
	now := time.Now().UTC()
	a, b := deadlineFixture(now)
	repo := &clockAccounts{choices: []model.Selection{a, b}, now: func() time.Time { return now }}
	reads := 0
	var paths []string
	tr := &Transport{Repository: repo, Provider: model.DRPC, PoolID: a.PoolID, Slot: "sepolia", Now: func() time.Time {
		reads++
		if reads == 4 {
			now = *a.CredentialsExpiresAt
		}
		return now
	}, Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		return response(r, 200, `{"result":"ok"}`), nil
	})}
	defer tr.CloseIdleConnections()
	req, _ := http.NewRequest(http.MethodPost, "https://lb.drpc.live/sepolia/", nil)
	res, err := tr.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	require.Equal(t, []string{"/sepolia/second"}, paths)
	require.Zero(t, repo.reports)
}
func TestCredentialDeadlineCancelsWireWorkWithoutReportingUpstreamFailure(t *testing.T) {
	now := time.Now().UTC()
	a, _ := deadlineFixture(now)
	end := now.Add(35 * time.Millisecond)
	a.CredentialsExpiresAt = &end
	repo := &clockAccounts{choices: []model.Selection{a}, now: time.Now}
	wire := 0
	tr := &Transport{Repository: repo, Provider: model.DRPC, PoolID: a.PoolID, Slot: "sepolia", Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		wire++
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	defer tr.CloseIdleConnections()
	req, _ := http.NewRequest(http.MethodPost, "https://lb.drpc.live/sepolia/", nil)
	_, err := tr.RoundTrip(req)
	require.ErrorIs(t, err, rpcpolicy.ErrCredentialsExpired)
	require.Equal(t, 1, wire)
	require.Zero(t, repo.reports)
}
func TestCredentialBodyCloseReleasesDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := &credentialBody{ReadCloser: io.NopCloser(strings.NewReader("ok")), cancel: cancel}
	require.NoError(t, body.Close())
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

// A response larger than the classification prefix remains bounded by expiry.
type deadlineResponseBody struct {
	ctx    context.Context
	prefix io.Reader
}

func (b *deadlineResponseBody) Read(p []byte) (int, error) {
	if b.prefix != nil {
		n, err := b.prefix.Read(p)
		if err != io.EOF {
			return n, err
		}
		b.prefix = nil
		if n > 0 {
			return n, nil
		}
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (*deadlineResponseBody) Close() error { return nil }

func TestCredentialExpiryDuringReturnedBodyIsTyped(t *testing.T) {
	now := time.Now().UTC()
	a, _ := deadlineFixture(now)
	end := now.Add(500 * time.Millisecond)
	a.CredentialsExpiresAt = &end
	repo := &clockAccounts{choices: []model.Selection{a}, now: time.Now}
	tr := &Transport{Repository: repo, Provider: model.DRPC, PoolID: a.PoolID, Slot: "sepolia", Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: &deadlineResponseBody{ctx: r.Context(), prefix: strings.NewReader(strings.Repeat(" ", 8192))}, Header: make(http.Header)}, nil
	})}
	defer tr.CloseIdleConnections()
	req, _ := http.NewRequest(http.MethodPost, "https://lb.drpc.live/sepolia/", nil)
	res, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	_, err = io.ReadAll(res.Body)
	require.ErrorIs(t, err, rpcpolicy.ErrCredentialsExpired)
	require.Zero(t, repo.reports)
}
