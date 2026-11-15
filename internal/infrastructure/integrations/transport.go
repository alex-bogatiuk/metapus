// Package integrations provides reusable tenant-local authenticated HTTP access.
package integrations

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/tenant"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/integrations/profiles"
	"metapus/internal/infrastructure/providerpolicy"
)

var ErrUnavailable = errors.New("integration accounts unavailable")
var ErrAccountChanged = errors.New("integration account changed; replay pagination window")
var ErrAttemptBudget = errors.New("integration account attempt budget reached")
var ErrAccessDenied = profiles.ErrAccessDenied

var wireRequests = func() metric.Int64Counter {
	counter, _ := otel.Meter("metapus/integrations").Int64Counter("integration.http.requests")
	return counter
}()

var upstreamResponses = func() metric.Int64Counter {
	counter, _ := otel.Meter("metapus/integrations").Int64Counter("integration.http.responses")
	return counter
}()

type Transport struct {
	Profiles    *profiles.Registry
	OriginalURL string
	Base        http.RoundTripper
	Repository  model.RuntimeRepository
	PoolID      id.ID
	Provider    model.Provider
	Slot        string
	// Target is optional for ordinary API calls; RPC bindings can refresh their URL/pool.
	Target    func(context.Context) (Binding, error)
	mu        sync.Mutex
	selected  model.Selection
	checkedAt time.Time
	Now       func() time.Time
	pacerMu   sync.Mutex
	pacers    map[string]*providerpolicy.Admission
}
type Binding struct {
	URL                  string
	PoolID               id.ID
	Provider             model.Provider
	Slot                 string
	MinRequestIntervalMS int
}

func (t *Transport) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now().UTC()
}
func (t *Transport) selectAccount(ctx context.Context, b Binding) (model.Selection, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if !t.selected.Recovery && t.selected.PoolID == b.PoolID && t.selected.Slot == b.Slot && now.Sub(t.checkedAt) < 10*time.Second && !selectionExpired(t.selected, now) {
		return t.selected, nil
	}
	s, err := t.Repository.Select(ctx, b.PoolID, b.Slot, b.Provider)
	if err != nil {
		var unavailable *model.PoolUnavailableError
		if errors.As(err, &unavailable) {
			if unavailable.Reason == model.Expired {
				return model.Selection{}, errors.Join(unavailable, providerpolicy.ErrCredentialsExpired)
			}
			return model.Selection{}, unavailable
		}
		// Authorization/configuration errors fail closed. Only transient DB errors
		// could justify a cache; avoid extending stale credentials on ambiguity.
		return model.Selection{}, fmt.Errorf("%w: account selection failed", ErrUnavailable)
	}
	t.selected = s
	t.checkedAt = now
	return s, nil
}
func (t *Transport) forget() { t.mu.Lock(); t.checkedAt = time.Time{}; t.mu.Unlock() }

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	b := Binding{PoolID: t.PoolID, Provider: t.Provider, Slot: t.Slot}
	if b.Slot == "" {
		b.Slot = "default"
	}
	if t.Target != nil {
		var err error
		b, err = t.Target(req.Context())
		if err != nil {
			return nil, fmt.Errorf("%w: RPC configuration unavailable", ErrUnavailable)
		}
		if b.Slot == "" {
			b.Slot = "default"
		}
	}
	profile, supported := t.Profiles.Lookup(b.Provider)
	if !supported {
		return nil, errors.New("integration profile does not support HTTP account pools")
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	var requestBody []byte
	if req.Body != nil {
		var err error
		requestBody, err = io.ReadAll(io.LimitReader(req.Body, (2<<20)+1))
		_ = req.Body.Close()
		if err != nil || len(requestBody) > 2<<20 {
			return nil, errors.New("integration request body unavailable or too large")
		}
	}
	// Pools contain at most 64 accounts. The caller's context bounds wire work;
	// the final selection distinguishes actual exhaustion from an attempt limit.
	for attempt := 0; attempt < 65; attempt++ {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		var s model.Selection
		if !id.IsNil(b.PoolID) {
			var err error
			s, err = t.selectAccount(req.Context(), b)
			if err != nil {
				return nil, err
			}
		}
		if selectionExpired(s, t.now()) {
			t.forget()
			continue
		}
		callCtx, cancelCredential := credentialContext(req.Context(), s, t.now())
		r := req.Clone(callCtx)
		u := *req.URL
		r.URL = &u
		r.Header = req.Header.Clone()
		if requestBody != nil {
			r.Body = io.NopCloser(bytes.NewReader(requestBody))
		}
		if b.URL != "" {
			target, err := url.Parse(b.URL)
			if err != nil || target.Host == "" || target.User != nil {
				cancelCredential()
				return nil, errors.New("invalid integration endpoint")
			}
			r.URL.Scheme = target.Scheme
			r.URL.Host = target.Host
			if profile.ReplaceTargetPath {
				r.URL.Path = strings.TrimRight(target.Path, "/") + "/"
				r.URL.RawPath = ""
			} else {
				original, _ := url.Parse(t.OriginalURL)
				prefix := ""
				if original != nil {
					prefix = strings.TrimRight(original.Path, "/")
				}
				r.URL.Path = strings.TrimRight(target.Path, "/") + strings.TrimPrefix(r.URL.Path, prefix)
			}
		}
		if err := profile.Authorize(r, s.Key); err != nil {
			cancelCredential()
			return nil, err
		}
		if interval := profile.MinRequestIntervalMS(); interval > 0 {
			if err := t.waitAccount(callCtx, b, s, interval); err != nil {
				if errors.Is(context.Cause(callCtx), providerpolicy.ErrCredentialsExpired) {
					cancelCredential()
					t.forget()
					continue
				}
				cancelCredential()
				return nil, errors.Join(providerpolicy.ErrAdmissionBusy, err)
			}
		}
		r.Host = ""
		pagination := strings.Contains(r.URL.Path, "/v1/") && (profile.PinsPagination || b.Provider == "" && t.Target != nil)
		if pagination {
			q := r.URL.Query()
			fingerprint := q.Get("fingerprint")
			if fingerprint != "" && !strings.HasPrefix(fingerprint, "mp1.") {
				cancelCredential()
				return nil, ErrAccountChanged
			}
			if strings.HasPrefix(fingerprint, "mp1.") {
				raw, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(fingerprint, "mp1."))
				if e != nil {
					cancelCredential()
					return nil, ErrAccountChanged
				}
				var pin pagePin
				if json.Unmarshal(raw, &pin) != nil || pin.Account != s.AccountID.String() || pin.Version != s.CredentialsVersion || pin.Slot != b.Slot || pin.Binding != b.URL {
					cancelCredential()
					return nil, ErrAccountChanged
				}
				q.Set("fingerprint", pin.Cursor)
				r.URL.RawQuery = q.Encode()
			}
		}
		if !id.IsNil(b.PoolID) {
			_, operation, _ := strings.Cut(req.URL.Path, "/wallet/")
			if operation == "" {
				_, operation, _ = strings.Cut(req.URL.Path, "/walletsolidity/")
			}
			if operation == "" || len(operation) > 64 || strings.ContainsAny(operation, "/?#") {
				operation = "api"
			}
			wireRequests.Add(req.Context(), 1, metric.WithAttributes(attribute.String("integration.provider", string(b.Provider)), attribute.String("integration.account_id", s.AccountID.String()), attribute.String("integration.tenant_id", tenant.GetTenantID(req.Context())), attribute.String("integration.operation", operation)))
		}
		if selectionExpired(s, t.now()) || errors.Is(context.Cause(callCtx), providerpolicy.ErrCredentialsExpired) {
			cancelCredential()
			t.forget()
			continue
		}
		resp, err := base.RoundTrip(r)
		if err != nil {
			expired := errors.Is(context.Cause(callCtx), providerpolicy.ErrCredentialsExpired)
			cancelCredential()
			if expired {
				t.forget()
				continue
			}
			return nil, errors.Join(errors.New("integration upstream transport failed"), req.Context().Err())
		}
		if s.CredentialsExpiresAt != nil {
			resp.Body = &credentialBody{ReadCloser: resp.Body, ctx: callCtx, cancel: cancelCredential}
		} else {
			cancelCredential()
		}
		limit := int64(8192)
		prefix, err := io.ReadAll(io.LimitReader(resp.Body, limit))
		if err != nil {
			expired := errors.Is(context.Cause(callCtx), providerpolicy.ErrCredentialsExpired)
			_ = resp.Body.Close()
			if expired {
				t.forget()
				continue
			}
			return nil, errors.New("integration upstream body failed")
		}
		failure := profile.Failure(resp.StatusCode, prefix)
		if !id.IsNil(b.PoolID) {
			var envelope struct {
				Error struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			_ = json.Unmarshal(prefix, &envelope)
			upstreamResponses.Add(req.Context(), 1, metric.WithAttributes(attribute.String("integration.provider", string(b.Provider)), attribute.String("integration.account_id", s.AccountID.String()), attribute.Int("http.response.status_code", resp.StatusCode), attribute.Int("integration.error_code", envelope.Error.Code), attribute.String("integration.failure", string(failure))))
		}
		if failure != "" && !id.IsNil(b.PoolID) {
			_ = resp.Body.Close()
			retry := profile.NextAttempt(resp.StatusCode, resp.Header, prefix, t.now())
			if err := t.Repository.Report(req.Context(), s, failure, retry); err != nil {
				return nil, errors.New("integration account state persistence failed")
			}
			t.forget()
			// A cursor cannot be replayed against another account. The caller must
			// retry its complete durable ingestion window instead.
			if req.URL.Query().Get("fingerprint") != "" {
				return nil, ErrAccountChanged
			}
			continue
		}
		if profile.ResponseError != nil {
			if err := profile.ResponseError(resp.StatusCode, prefix); err != nil {
				_ = resp.Body.Close()
				return nil, err
			}
		}
		if s.Recovery && resp.StatusCode >= 200 && resp.StatusCode < 300 && !hasProviderError(prefix) {
			if err := t.Repository.Report(req.Context(), s, "", time.Time{}); err != nil {
				_ = resp.Body.Close()
				return nil, errors.New("integration recovery persistence failed")
			}
			t.forget()
		}
		if resp.Header == nil {
			resp.Header = make(http.Header)
		}
		resp.Header.Del(responsePinHeader)
		if pagination {
			pin, _ := json.Marshal(pagePin{Account: s.AccountID.String(), Version: s.CredentialsVersion, Slot: b.Slot, Binding: b.URL})
			resp.Header.Set(responsePinHeader, base64.RawURLEncoding.EncodeToString(pin))
		}

		resp.Body = &prefixBody{Reader: io.MultiReader(bytes.NewReader(prefix), resp.Body), Closer: resp.Body}
		return resp, nil
	}
	return nil, ErrAttemptBudget
}

func (t *Transport) waitAccount(ctx context.Context, b Binding, s model.Selection, interval int) error {
	// Slots, bindings and overlapping generations share the account pacer.
	key := "integration-account/" + tenant.GetTenantID(ctx) + "/" + s.AccountID.String()
	t.pacerMu.Lock()
	if t.pacers == nil {
		t.pacers = make(map[string]*providerpolicy.Admission)
	}
	pacer := t.pacers[key]
	if pacer == nil {
		var err error
		pacer, err = providerpolicy.Acquire(key, providerpolicy.Capacity{Concurrency: 2, BulkConcurrency: 1, RequestsPerSecond: 1000, MinRequestIntervalMS: max(interval, b.MinRequestIntervalMS)})
		if err != nil {
			t.pacerMu.Unlock()
			return errors.New("conflicting integration account request limits")
		}
		t.pacers[key] = pacer
	}
	t.pacerMu.Unlock()
	return pacer.Wait(ctx)
}

type prefixBody struct {
	io.Reader
	io.Closer
}
type pagePin struct {
	Account string `json:"account"`
	Version int    `json:"version"`
	Cursor  string `json:"cursor"`
	Slot    string `json:"slot"`
	Binding string `json:"binding"`
}

// Classify remains available to diagnostic callers; the transport uses its
// injected registry so classification and authorization always select one profile.
func Classify(provider model.Provider, status int, body []byte) model.Failure {
	profile, ok := (*profiles.Registry)(nil).Lookup(provider)
	if !ok {
		return ""
	}
	return profile.Failure(status, body)
}

func providerRetryAt(provider model.Provider, header string, body []byte, now time.Time) time.Time {
	profile, _ := (*profiles.Registry)(nil).Lookup(provider)
	headers := make(http.Header)
	headers.Set("Retry-After", header)
	return profile.NextAttempt(0, headers, body, now)
}
func retryAt(value string, now time.Time) time.Time { return profiles.RetryAt(value, now) }

func hasProviderError(body []byte) bool {
	var v struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			Result *bool `json:"result"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &v) != nil {
		return false
	}
	return len(v.Error) > 0 && string(v.Error) != "null" || v.Result.Result != nil && !*v.Result.Result
}
func (t *Transport) CloseIdleConnections() {
	t.pacerMu.Lock()
	for key, pacer := range t.pacers {
		providerpolicy.Release(pacer)
		delete(t.pacers, key)
	}
	t.pacerMu.Unlock()
	if c, ok := t.Base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

const responsePinHeader = "X-Metapus-Integration-Pin"

// PinResponseCursor is called by the typed pagination adapter after its normal
// bounded decode. The HTTP transport never buffers or rewrites a full page.
func PinResponseCursor(headers http.Header, cursor string) (string, error) {
	encoded := headers.Get(responsePinHeader)
	if encoded == "" || cursor == "" {
		return cursor, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrAccountChanged
	}
	var pin pagePin
	if json.Unmarshal(raw, &pin) != nil {
		return "", ErrAccountChanged
	}
	pin.Cursor = cursor
	raw, err = json.Marshal(pin)
	if err != nil {
		return "", ErrAccountChanged
	}
	return "mp1." + base64.RawURLEncoding.EncodeToString(raw), nil
}
