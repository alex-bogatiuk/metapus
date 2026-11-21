package crypto_worker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestOptionalExecutionFacadesFailClosedWhenAbsent(t *testing.T) {
	var funding *NativeFundingService
	ready, err := funding.Ensure(t.Context(), nil, crypto.NetworkPolicy{}, NativeFundingRequest{}, nil, types.ZeroRawAmount())
	require.False(t, ready)
	require.ErrorIs(t, err, ErrNativeFundingHeld)
	var operations *CryptoNetworkOperationCoordinator
	_, err = operations.EnsureActivationBroadcast(t.Context(), AccountActivationTask{}, "saved-hash")
	require.Error(t, err)
	_, err = operations.FinalizeActivation(t.Context(), id.New(), id.New(), crypto.ChainReceipt{})
	require.Error(t, err)
}
