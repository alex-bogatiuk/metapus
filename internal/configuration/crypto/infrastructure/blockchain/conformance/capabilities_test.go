package conformance

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/evm"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/tron"
)

func TestInstalledFamiliesExportCompleteCapabilities(t *testing.T) {
	tronCaps := (&tron.Factory{}).Capabilities()
	evmCaps := (&evm.Factory{}).Capabilities()
	require.NoError(t, tronCaps.Validate())
	require.NoError(t, evmCaps.Validate())

	require.Equal(t, crypto.ChainFamilyTRON, tronCaps.Family)
	require.Equal(t, crypto.SequenceNone, tronCaps.SequenceModel)
	require.Equal(t, crypto.CheckpointUnixMillis, tronCaps.CheckpointTimeUnit)
	require.Equal(t, crypto.TokenAccountSameAddress, tronCaps.TokenAccountModel)
	require.Equal(t, crypto.WalletProvisioningDerivedWithActivation, tronCaps.WalletProvisioning)
	require.Equal(t, crypto.ExternalResourceOptional, tronCaps.ExternalResourceMode)
	require.False(t, tronCaps.SupportsReplacement)
	require.False(t, tronCaps.RequiresNetworkSequence())
	require.Equal(t, 60, tronCaps.MinWatcherLagSeconds)
	require.ElementsMatch(t, []string{"fullnode", "solidity"}, tronCaps.RequiredRPCRoles)

	require.Equal(t, crypto.ChainFamilyEVM, evmCaps.Family)
	require.Equal(t, crypto.SequenceAccount, evmCaps.SequenceModel)
	require.Equal(t, crypto.CheckpointUnixSeconds, evmCaps.CheckpointTimeUnit)
	require.Equal(t, crypto.TokenAccountSameAddress, evmCaps.TokenAccountModel)
	require.Equal(t, crypto.WalletProvisioningDerived, evmCaps.WalletProvisioning)
	require.Equal(t, crypto.ExternalResourceNone, evmCaps.ExternalResourceMode)
	require.True(t, evmCaps.SupportsReplacement)
	require.True(t, evmCaps.RequiresNetworkSequence())

	require.Equal(t, crypto.SigningAuthorizationSequenceLessSchemaVersion, crypto.SigningAuthorizationSchemaVersionFor(tronCaps.SequenceModel))
	require.Equal(t, crypto.SigningAuthorizationSchemaVersion, crypto.SigningAuthorizationSchemaVersionFor(evmCaps.SequenceModel))
}

func TestFinanciallyFinalRequiresCanonicalSettledReceipt(t *testing.T) {
	receipt := crypto.ChainReceipt{
		Canonicality: crypto.CanonicalityCanonical,
		Finality:     crypto.FinalitySolidified,
		Status:       crypto.ReceiptSucceeded,
	}
	require.True(t, receipt.FinanciallyFinal())
	receipt.Finality = crypto.FinalityProvisional
	require.False(t, receipt.FinanciallyFinal())
	receipt.Finality = crypto.FinalityFinalized
	receipt.Canonicality = crypto.CanonicalityOrphaned
	require.False(t, receipt.FinanciallyFinal())
}
