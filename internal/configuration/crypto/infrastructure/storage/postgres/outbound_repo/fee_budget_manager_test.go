package outbound_repo

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
)

func TestActivationFeeSettlementBelongsToNetworkOperationCoordinator(t *testing.T) {
	require.Error(t, validateFeeSettlementOwner(crypto.SigningPurposeActivation, true))
	require.NoError(t, validateFeeSettlementOwner(crypto.SigningPurposeActivation, false))
	require.NoError(t, validateFeeSettlementOwner(crypto.SigningPurposeSweep, true))
}
