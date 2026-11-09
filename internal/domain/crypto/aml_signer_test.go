package crypto

import (
	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"testing"
)

func TestAMLSigningContractRejectsLegacyAndIncompleteImplementations(t *testing.T) {
	valid := AMLSigningContract{Schema: AMLSigningSchemaV1, IntentSchema: MoneyMovementIntentV2, SourceLineBasis: "CryptoAMLWithdrawalLine"}
	require.NoError(t, valid.Validate())
	for _, contract := range []AMLSigningContract{{}, {Schema: "aml_signing.v2", IntentSchema: valid.IntentSchema, SourceLineBasis: valid.SourceLineBasis}, {Schema: valid.Schema, IntentSchema: MoneyMovementIntentV1, SourceLineBasis: valid.SourceLineBasis}, {Schema: valid.Schema, IntentSchema: valid.IntentSchema, SourceLineBasis: "WithdrawalRequest"}} {
		require.Error(t, contract.Validate())
	}
}

func TestAMLConfigurationCanBePreparedWithDispatchPaused(t *testing.T) {
	n := AMLNetworkConfiguration{Network: NetworkRef{NativeTokenID: id.New()}, Active: true, CustodyEnforced: true, EnabledScopes: 2, Merchants: 1}
	require.Empty(t, n.ConfigurationBlockers(), "preparing configuration does not enable network dispatch")
	n.MissingNativeScopes = 1
	n.LegacyPendingWithdrawals = 1
	n.BaselineNeedsAttention = 1
	require.ElementsMatch(t, []string{"native_scopes_missing", "legacy_withdrawals_pending", "baseline_needs_attention"}, n.ConfigurationBlockers())
}
