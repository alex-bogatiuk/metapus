package crypto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

func TestValidateResourceProviderCode(t *testing.T) {
	require.NoError(t, ValidateResourceProviderCode("itrx"))
	require.NoError(t, ValidateResourceProviderCode("tron_save"))
	require.Error(t, ValidateResourceProviderCode(""))
	require.Error(t, ValidateResourceProviderCode("ITRX"))
	require.Error(t, ValidateResourceProviderCode("1trx"))
}

func TestExternalResourcePolicyRequiresProvidersForRental(t *testing.T) {
	policy := &ExternalResourcePolicy{
		Strategy: ResourcePreferRental, ProviderFailureThreshold: 3, ProviderOpenDuration: time.Minute,
	}
	require.ErrorContains(t, policy.Validate(), "at least one provider")
	accountID := id.New()
	policy.ProviderAccountIDs = []id.ID{accountID}
	require.NoError(t, policy.Validate())
	policy.ProviderAccountIDs = []id.ID{accountID, accountID}
	require.ErrorContains(t, policy.Validate(), "duplicate")
	policy.ProviderAccountIDs = []id.ID{id.Nil()}
	require.ErrorContains(t, policy.Validate(), "cannot be nil")
	policy.Strategy = ResourceBurnOnly
	policy.ProviderAccountIDs = nil
	require.NoError(t, policy.Validate())
}

func TestResourceOrderStatusTerminal(t *testing.T) {
	require.False(t, ResourceOrderActive.Terminal())
	require.True(t, ResourceOrderClosed.Terminal())
	require.True(t, ResourceOrderFailed.Terminal())
}
