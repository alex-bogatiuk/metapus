package webhooksecret

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/infrastructure/vaultclient"
)

func TestDeleteIsIdempotentWhenVaultMetadataIsAbsent(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client, err := vaultclient.New(vaultclient.Config{Addr: server.URL, MaxRetries: 1})
	require.NoError(t, err)
	store, err := NewVaultStore(client, "secret")
	require.NoError(t, err)

	require.NoError(t, store.Delete(context.Background(), "merchant-webhooks/m/e/v1"))
}
