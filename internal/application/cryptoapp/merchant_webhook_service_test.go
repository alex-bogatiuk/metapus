package cryptoapp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
)

func TestWebhookRotatedSecretRefIsUniqueAndTenantScoped(t *testing.T) {
	merchantID := id.New()
	endpointID := id.New()
	first := webhookRotatedSecretRef(merchantID, endpointID)
	second := webhookRotatedSecretRef(merchantID, endpointID)

	require.NotEqual(t, first, second)
	require.Contains(t, first, merchantID.String())
	require.Contains(t, first, endpointID.String())
	require.NotContains(t, first, "..")
}
