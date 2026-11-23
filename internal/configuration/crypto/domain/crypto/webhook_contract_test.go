package crypto

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
)

func TestInvoiceCompletedGoldenWebhookFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../../../api/fixtures/invoice.completed.json")
	require.NoError(t, err)

	var fixture WebhookPayload
	require.NoError(t, json.Unmarshal(raw, &fixture))
	require.Equal(t, "evt_01K3MZ7V6X4H8Q2T9N5R1C0BFA", fixture.ID)
	require.Equal(t, MerchantWebhookAPIVersion, fixture.APIVersion)
	require.Equal(t, WebhookInvoiceCompleted, fixture.Type)
	require.Equal(t, time.Date(2026, 8, 26, 6, 12, 3, 0, time.UTC), fixture.CreatedAt)

	object := fixture.Data.Object
	require.Equal(t, "completed", object["status"])
	require.Equal(t, "100000000", object["expectedAmount"])
	require.Equal(t, "100000000", object["creditedAmount"])
	require.Equal(t, "USDT-TRC20", object["currency"])
	require.Equal(t, "TRON-NILE", object["network"])
	require.NotContains(t, object, "resource_version")
	require.NotContains(t, object, "credited_amount")
}

func TestPublicWebhookEventCatalogKeepsViewedInternal(t *testing.T) {
	require.False(t, IsKnownWebhookEventType(WebhookEventType("invoice.viewed")))
	require.True(t, IsKnownWebhookEventType(WebhookWithdrawalConfirmed))
	require.True(t, IsKnownWebhookEventType(WebhookWithdrawalFailed))
}
