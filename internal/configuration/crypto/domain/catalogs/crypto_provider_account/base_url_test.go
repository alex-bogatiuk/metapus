package crypto_provider_account

import (
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
)

func TestValidateBaseURL(t *testing.T) {
	for _, valid := range []string{"https://nile.itrx.io", "https://itrx.io/", "http://127.0.0.1:8080", "http://localhost:9000", "https://api.itrx.io/v2"} {
		require.NoError(t, ValidateBaseURL(valid), valid)
	}
	for _, invalid := range []string{"https://https//nile.itrx.io", "https//nile.itrx.io", "nile.itrx.io", "ftp://itrx.io", "https://itrx", "https://user:pw@itrx.io", "https://itrx.io//api", "https://itrx.io?x=1", "https://.itrx.io"} {
		require.Error(t, ValidateBaseURL(invalid), invalid)
	}
}

func TestProviderAccountRejectsSubmittedMalformedBaseURL(t *testing.T) {
	account := New("CPA-1", "iTRX", "itrx", id.New(), id.New())
	account.SubmittedBaseURL = "https://https//nile.itrx.io"
	require.ErrorContains(t, account.Validate(t.Context()), "base URL")
	account.SubmittedBaseURL = "https://nile.itrx.io"
	require.NoError(t, account.Validate(t.Context()))
}
