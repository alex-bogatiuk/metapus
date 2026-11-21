package itrx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/json"
)

func TestHMACSHA256HexMatchesITRXDocs(t *testing.T) {
	const secret = "0285A2741D0E76E2E187260EB23E51851D48403A756333E7D0CF845406ABF3E8"
	const message = `1686796826&{"callback_url":"http://{mydomain}/callback","energy_amount":32000,"out_trade_no":"123456","period":"1D","receive_address":"TExWKszFWYTKZH8LYiovAPKzS3L9MLZ4kw"}`

	got := hmacSHA256Hex(message, secret)

	assert.Equal(t, "07c125cabfe006614a02131f927cca9e7a654f7644a817b155fad591e1c50f7b", got)
}

func TestAdapterPlaceUsesCanonicalJSON(t *testing.T) {
	const (
		apiKey    = "api-key"
		apiSecret = "api-secret"
		timestamp = "1686796826"
		wantBody  = `{"active":0,"callback_url":"http://example.test/callback?a=1&b=2","energy_amount":65000,"exclusive":0,"out_trade_no":"order-1","period":"1H","receive_address":"TReceive"}`
	)
	wantSignature := hmacSHA256Hex(timestamp+"&"+wantBody, apiSecret)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/frontend/order", r.URL.Path)
		assert.Equal(t, apiKey, r.Header.Get("API-KEY"))
		assert.Equal(t, timestamp, r.Header.Get("TIMESTAMP"))
		assert.Equal(t, wantSignature, r.Header.Get("SIGNATURE"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var raw json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		assert.JSONEq(t, wantBody, string(raw))
		assert.Equal(t, wantBody, string(raw))

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errno":  0,
			"serial": "serial-1",
			"amount": 250000,
		})
	}))
	defer server.Close()

	adapter, err := New(Config{
		BaseURL:     server.URL,
		APIKey:      apiKey,
		APISecret:   apiSecret,
		CallbackURL: "http://example.test/callback?a=1&b=2",
		Now:         func() time.Time { return time.Unix(1686796826, 0) },
	})
	require.NoError(t, err)

	order, err := adapter.Place(context.Background(), crypto.ResourceOrderIntent{
		ClientOrderID:  "order-1",
		ResourceKind:   crypto.ResourceKindEnergy,
		Quantity:       65000,
		ReceiveAddress: "TReceive",
		Period:         "1H",
	})
	require.NoError(t, err)
	assert.Equal(t, "serial-1", order.ExternalID)
	assert.Equal(t, "250000", order.CostAmount.String())
	assert.Equal(t, crypto.ResourceOrderActive, order.Status)
	assert.Equal(t, crypto.ResourceProviderITRX, order.Provider)
}

func TestAdapterPlaceAndReclaimUseCanonicalSignedBodies(t *testing.T) {
	const timestamp = "1686796826"
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var raw json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		body := string(raw)
		assert.Equal(t, hmacSHA256Hex(timestamp+"&"+body, "secret"), r.Header.Get("SIGNATURE"))
		switch r.URL.Path {
		case "/api/v1/frontend/order":
			assert.Equal(t, `{"active":0,"callback_url":"https://example.test/callback","energy_amount":65000,"exclusive":0,"out_trade_no":"rr-1","period":"1H","receive_address":"TSource"}`, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"errno": 0, "serial": "serial-1", "amount": 250000, "balance": 900000})
		case "/api/v1/frontend/order/reclaim":
			assert.Equal(t, `{"serial":"serial-1"}`, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"errno": 0})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	adapter, err := New(Config{
		BaseURL: server.URL, APIKey: "key", APISecret: "secret",
		CallbackURL: "https://example.test/callback",
		Now:         func() time.Time { return time.Unix(1686796826, 0) },
	})
	require.NoError(t, err)
	order, err := adapter.Place(context.Background(), crypto.ResourceOrderIntent{
		ClientOrderID: "rr-1", Quantity: 65000, ReceiveAddress: "TSource", Period: "1H",
	})
	require.NoError(t, err)
	assert.Equal(t, "serial-1", order.ExternalID)
	_, err = adapter.Reclaim(context.Background(), crypto.ResourceOrderRef{ExternalID: "serial-1"})
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestAdapterMapsSentinelErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/frontend/order/query" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	adapter, err := New(Config{BaseURL: server.URL, APIKey: "key", APISecret: "secret"})
	require.NoError(t, err)
	_, err = adapter.Place(context.Background(), crypto.ResourceOrderIntent{
		ClientOrderID: "rr-1", Quantity: 65000, ReceiveAddress: "TSource",
	})
	require.ErrorIs(t, err, crypto.ErrResourceProviderTransient)
	_, err = adapter.Query(context.Background(), crypto.ResourceOrderRef{ClientOrderID: "rr-1"})
	require.ErrorIs(t, err, crypto.ErrResourceProviderUnauthorized)
}

func TestAdapterQueryMapsTerminalStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errno": 0, "order_no": "serial-failed", "energy_amount": 65000,
			"amount": 300000, "refund_amount": 100000, "status": 43,
		})
	}))
	defer server.Close()
	adapter, err := New(Config{BaseURL: server.URL, APIKey: "key", APISecret: "secret"})
	require.NoError(t, err)
	order, err := adapter.Query(context.Background(), crypto.ResourceOrderRef{ClientOrderID: "rr-failed"})
	require.NoError(t, err)
	assert.Equal(t, crypto.ResourceOrderFailed, order.Status)
	assert.Equal(t, "serial-failed", order.ExternalID)
	assert.Equal(t, "100000", order.RefundAmount.String())
}

func TestNewRejectsPartialCredentialsAndPlaintextProduction(t *testing.T) {
	_, err := New(Config{APIKey: "key"})
	require.Error(t, err)
	_, err = New(Config{APIKey: "key", APISecret: "secret"})
	require.Error(t, err)
	_, err = New(Config{BaseURL: "http://itrx.internal", APIKey: "key", APISecret: "secret", AppEnv: "production"})
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://key:secret@itrx.example", APIKey: "key", APISecret: "secret"})
	require.Error(t, err)
	// The stand stored a duplicated scheme; url.Parse reads its host as "https".
	_, err = New(Config{BaseURL: "https://https//nile.itrx.io", APIKey: "key", APISecret: "secret"})
	require.ErrorContains(t, err, "base URL")
	_, err = New(Config{BaseURL: "https://nile.itrx.io", APIKey: "key", APISecret: "secret", AppEnv: "production"})
	require.NoError(t, err)
}

func TestCapabilitiesValidate(t *testing.T) {
	require.NoError(t, Capabilities().Validate())
}

func TestVendorErrnoDoesNotBecomeSentinel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errno": 7, "message": "no energy"})
	}))
	defer server.Close()
	adapter, err := New(Config{BaseURL: server.URL, APIKey: "key", APISecret: "secret"})
	require.NoError(t, err)
	_, err = adapter.Place(context.Background(), crypto.ResourceOrderIntent{
		ClientOrderID: "rr-1", Quantity: 65000, ReceiveAddress: "TSource",
	})
	require.Error(t, err)
	require.False(t, errors.Is(err, crypto.ErrResourceProviderTransient))
	require.False(t, errors.Is(err, crypto.ErrResourceProviderInvalidReply))
	require.False(t, errors.Is(err, crypto.ErrResourceProviderUnauthorized))
}
