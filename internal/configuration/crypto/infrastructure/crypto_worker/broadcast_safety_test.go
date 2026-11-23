package crypto_worker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
)

func TestExactPersistedHashMakesAmbiguousBroadcastTrackable(t *testing.T) {
	network := crypto.NetworkRef{Family: crypto.ChainFamilyEVM}
	outcome := crypto.BroadcastOutcome{Status: crypto.BroadcastAmbiguous, TxHash: "0xABC"}
	require.True(t, trackableAmbiguousBroadcast(network, outcome, "0xabc"))
	require.False(t, trackableAmbiguousBroadcast(network, outcome, "0xdef"))
	require.True(t, trackableAmbiguousBroadcast(crypto.NetworkRef{Family: crypto.ChainFamilyTRON}, outcome, "0xabc"))
	outcome.TxHash = ""
	require.False(t, trackableAmbiguousBroadcast(crypto.NetworkRef{Family: crypto.ChainFamilyTRON}, outcome, "0xabc"))
	outcome.TxHash = "0xABC"
	outcome.Status = crypto.BroadcastRejected
	require.False(t, trackableAmbiguousBroadcast(network, outcome, "0xabc"))
}
