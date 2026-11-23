package vaultclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientTokenModes(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{name: "proxy tokenless"},
		{name: "direct dev token", token: "dev-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.token, r.Header.Get("X-Vault-Token"))
				assert.Equal(t, "/v1/test", r.URL.Path)
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]bool{"ok": true}})
			}))
			defer server.Close()

			client, err := New(Config{Addr: server.URL, Token: tt.token, MaxRetries: 1})
			require.NoError(t, err)
			var response struct {
				Data struct {
					OK bool `json:"ok"`
				} `json:"data"`
			}
			require.NoError(t, client.DoJSON(context.Background(), http.MethodGet, "/v1/test", nil, &response))
			assert.True(t, response.Data.OK)
		})
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":"too large"}`))
	}))
	defer server.Close()
	client, err := New(Config{Addr: server.URL, MaxRetries: 1, MaxResponseBytes: 8})
	require.NoError(t, err)
	err = client.DoJSON(context.Background(), http.MethodGet, "/v1/test", nil, &map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestClientRejectsUntrustedPath(t *testing.T) {
	client, err := New(Config{Addr: "http://vault-proxy:8100", MaxRetries: 1})
	require.NoError(t, err)
	err = client.DoJSON(context.Background(), http.MethodGet, "https://attacker.example/v1/test", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Vault API path")
}

func TestClientPreservesHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client, err := New(Config{Addr: server.URL, MaxRetries: 1})
	require.NoError(t, err)

	err = client.DoJSON(context.Background(), http.MethodDelete, "/v1/secret/metadata/missing", nil, nil)
	require.Error(t, err)
	assert.True(t, IsHTTPStatus(err, http.StatusNotFound))
	assert.False(t, IsHTTPStatus(err, http.StatusForbidden))
}
