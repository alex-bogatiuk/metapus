package cryptohandlers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeWithdrawalRequestStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []string{
		"active", "pending_signature", "pending_approval", "approved", "signing", "broadcast",
		"confirmed", "rejected", "failed",
	} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			actual, err := normalizeWithdrawalRequestStatus(status)
			require.NoError(t, err)
			require.Equal(t, status, actual)
		})
	}
}

func TestNormalizeWithdrawalRequestStatusDefaultsToAll(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"", "all", " ALL "} {
		actual, err := normalizeWithdrawalRequestStatus(status)
		require.NoError(t, err)
		require.Equal(t, "all", actual)
	}
}

func TestNormalizeWithdrawalRequestStatusRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	_, err := normalizeWithdrawalRequestStatus("completed")
	require.Error(t, err)
}
