package cryptodto

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/crypto_provider_account"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/secretbox"
)

const (
	providerKey      = "0123456789abcdef0123456789abcdef"
	otherProviderKey = "fedcba9876543210fedcba9876543210"
)

func providerRequest() CreateCryptoProviderAccountRequest {
	return CreateCryptoProviderAccountRequest{
		Code: "CPA-1", Name: "iTRX", Provider: "itrx",
		NetworkID: id.New().String(), BalanceTokenID: id.New().String(),
		APIKey: "key-123456789", APISecret: "secret",
	}
}

func openProviderCredentials(t *testing.T, sealed []byte) crypto_provider_account.ProviderCredentials {
	t.Helper()
	raw, err := secretbox.Open(sealed)
	require.NoError(t, err)
	var creds crypto_provider_account.ProviderCredentials
	require.NoError(t, json.Unmarshal(raw, &creds))
	return creds
}

func requireValidationCode(t *testing.T, err error) {
	t.Helper()
	var appErr *apperror.AppError
	require.True(t, errors.As(err, &appErr), "%v", err)
	require.Equal(t, apperror.NewValidation("").Code, appErr.Code)
}

func TestProviderAccountCreateRejectsCredentialsWithoutKey(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "")
	account := providerRequest().ToEntity()

	require.Empty(t, account.CredentialsEnc)
	err := account.Validate(t.Context())
	requireValidationCode(t, err)
	require.ErrorIs(t, err, secretbox.ErrNotConfigured)
}

func TestProviderAccountCreateWithoutCredentialsNeedsNoKey(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "")
	request := providerRequest()
	request.APIKey, request.APISecret = "", ""
	account := request.ToEntity()

	require.NoError(t, account.Validate(t.Context()))
	require.Empty(t, account.CredentialsEnc)
}

func TestProviderAccountCreateSealsCredentials(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", providerKey)
	account := providerRequest().ToEntity()

	require.NoError(t, account.Validate(t.Context()))
	require.NotNil(t, account.CredentialsUpdatedAt)
	creds := openProviderCredentials(t, account.CredentialsEnc)
	require.Equal(t, "key-123456789", creds.APIKey)
	require.Equal(t, "secret", creds.APISecret)
}

func TestProviderAccountUpdateKeepsStoredCredentials(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", providerKey)
	account := providerRequest().ToEntity()
	version := account.CredentialsVersion

	update := UpdateCryptoProviderAccountRequest{CreateCryptoProviderAccountRequest: providerRequest(), Version: 1}
	update.APIKey, update.APISecret, update.BaseURL = "", "", "https://nile.itrx.io"
	update.ApplyTo(account)

	require.NoError(t, account.Validate(t.Context()))
	require.Equal(t, version+1, account.CredentialsVersion)
	creds := openProviderCredentials(t, account.CredentialsEnc)
	require.Equal(t, "key-123456789", creds.APIKey)
	require.Equal(t, "secret", creds.APISecret)
	require.Equal(t, "https://nile.itrx.io", creds.BaseURL)
}

func TestProviderAccountUpdateRejectsUnreadableCredentials(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		validation bool
	}{
		{name: "key not configured", key: "", validation: true},
		{name: "another key", key: otherProviderKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AUTOMATION_ENCRYPTION_KEY", providerKey)
			account := providerRequest().ToEntity()
			stored, version := account.CredentialsEnc, account.CredentialsVersion

			t.Setenv("AUTOMATION_ENCRYPTION_KEY", tt.key)
			update := UpdateCryptoProviderAccountRequest{CreateCryptoProviderAccountRequest: providerRequest(), Version: 1}
			update.APISecret = ""
			update.ApplyTo(account)

			err := account.Validate(t.Context())
			require.Error(t, err)
			if tt.validation {
				requireValidationCode(t, err)
			} else {
				var appErr *apperror.AppError
				require.True(t, errors.As(err, &appErr), "%v", err)
				require.Equal(t, apperror.NewInternal(nil).Code, appErr.Code)
			}
			require.Equal(t, stored, account.CredentialsEnc)
			require.Equal(t, version, account.CredentialsVersion)
		})
	}
}
