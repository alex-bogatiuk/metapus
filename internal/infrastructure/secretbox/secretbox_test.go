package secretbox_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/secretbox"
)

func TestSealOpensWithTheSameKey(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	require.NoError(t, secretbox.Ready())

	sealed, err := secretbox.Seal([]byte("token"))
	require.NoError(t, err)
	require.NotContains(t, string(sealed), "token")

	opened, err := secretbox.Open(sealed)
	require.NoError(t, err)
	require.Equal(t, "token", string(opened))
}

func TestOpenRejectsAnotherKey(t *testing.T) {
	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	sealed, err := secretbox.Seal([]byte("token"))
	require.NoError(t, err)

	t.Setenv("AUTOMATION_ENCRYPTION_KEY", "fedcba9876543210fedcba9876543210")
	_, err = secretbox.Open(sealed)
	require.Error(t, err)
}

func TestKeyErrors(t *testing.T) {
	tests := []struct {
		name          string
		give          string
		notConfigured bool
	}{
		{name: "not set", give: "", notConfigured: true},
		{name: "wrong length", give: "short"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AUTOMATION_ENCRYPTION_KEY", tt.give)
			for _, err := range []error{secretbox.Ready(), sealErr(), openErr()} {
				var appErr *apperror.AppError
				require.True(t, errors.As(err, &appErr), "%v", err)
				require.Equal(t, apperror.NewValidation("").Code, appErr.Code)
				require.Equal(t, tt.notConfigured, errors.Is(err, secretbox.ErrNotConfigured))
			}
		})
	}
}

func sealErr() error { _, err := secretbox.Seal([]byte("x")); return err }

func openErr() error { _, err := secretbox.Open([]byte("x")); return err }
