package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateProductionVaultEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "tokenless transit", env: map[string]string{"APP_ENV": "production", "CRYPTO_AUTH_SIGNER": "transit", "VAULT_ADDR": "http://vault-proxy:8100", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "https://otel-collector:4317"}},
		{name: "static token rejected", env: map[string]string{"APP_ENV": "production", "VAULT_TOKEN": "token"}, wantErr: "VAULT_TOKEN is forbidden"},
		{name: "static token file rejected", env: map[string]string{"APP_ENV": "production", "VAULT_TOKEN_FILE": "/run/secrets/vault-token"}, wantErr: "VAULT_TOKEN_FILE is forbidden"},
		{name: "private key rejected", env: map[string]string{"APP_ENV": "production", "CRYPTO_SIGNING_AUTH_PRIVATE_KEY": "key"}, wantErr: "PRIVATE_KEY is forbidden"},
		{name: "private key file rejected", env: map[string]string{"APP_ENV": "production", "CRYPTO_SIGNING_AUTH_PRIVATE_KEY_FILE": "/run/secrets/auth-key"}, wantErr: "PRIVATE_KEY_FILE is forbidden"},
		{name: "local rejected", env: map[string]string{"APP_ENV": "production", "CRYPTO_AUTH_SIGNER": "local"}, wantErr: "must be transit"},
		{name: "transit requires proxy address", env: map[string]string{"APP_ENV": "production", "CRYPTO_AUTH_SIGNER": "transit"}, wantErr: "VAULT_ADDR is required"},
		{name: "production requires metric export", env: map[string]string{"APP_ENV": "production", "VAULT_ADDR": "http://vault-proxy:8100"}, wantErr: "OTEL_EXPORTER_OTLP"},
		{name: "development direct mode", env: map[string]string{"APP_ENV": "development", "VAULT_TOKEN": "token", "CRYPTO_AUTH_SIGNER": "local"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProductionVaultEnvironment(func(key string) string { return tt.env[key] })
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}
