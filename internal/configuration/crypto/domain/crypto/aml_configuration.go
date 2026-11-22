package crypto

import (
	"metapus/pkg/sdk/id"
	"time"
)

// Configuration readiness is a rollout diagnostic, never a dispatch permit.
// Ingestion, physical reconciliation and exact source checks remain required.
type AMLConfigurationReport struct {
	ObservedAt        time.Time                 `json:"observedAt"`
	DatabaseSchema    int                       `json:"databaseSchema"`
	ExpectedSchema    int                       `json:"expectedSchema"`
	NamespaceEnrolled bool                      `json:"namespaceEnrolled"`
	Ready             bool                      `json:"ready"`
	Blockers          []string                  `json:"blockers"`
	Networks          []AMLNetworkConfiguration `json:"networks"`
}

type AMLNetworkConfiguration struct {
	Network                     NetworkRef `json:"network"`
	Active                      bool       `json:"active"`
	CustodyEnforced             bool       `json:"custodyEnforced"`
	DispatchEnabled             bool       `json:"dispatchEnabled"`
	NetworkHeld                 bool       `json:"networkHeld"`
	EmulatedScopes              int        `json:"emulatedScopes"`
	EnabledScopes               int        `json:"enabledScopes"`
	Merchants                   int        `json:"merchants"`
	MissingNativeScopes         int        `json:"missingNativeScopes"`
	UnownedPrincipalWallets     int        `json:"unownedPrincipalWallets"`
	UnplannedPendingWithdrawals int        `json:"unplannedPendingWithdrawals"`
	UnbaselinedAutomaticWallets int        `json:"unbaselinedAutomaticWallets"`
	BaselinePending             int        `json:"baselinePending"`
	BaselineNeedsAttention      int        `json:"baselineNeedsAttention"`
	SignerReady                 bool       `json:"signerReady"`
	Blockers                    []string   `json:"blockers"`
}

func (n AMLNetworkConfiguration) ConfigurationBlockers() []string {
	blockers := []string{}
	for _, check := range []struct {
		failed bool
		code   string
	}{
		{!n.Active, "network_inactive"},
		{id.IsNil(n.Network.NativeTokenID), "native_asset_missing"},
		{!n.CustodyEnforced, "custody_not_enforced"},
		{n.EnabledScopes == 0, "aml_scopes_not_enabled"},
		{n.NetworkHeld, "network_hold_open"},
		{n.MissingNativeScopes > 0, "native_scopes_missing"},
		{n.UnownedPrincipalWallets > 0, "legacy_principal_inventory"},
		{n.UnplannedPendingWithdrawals > 0, "withdrawals_without_aml_plan"},
		{n.UnbaselinedAutomaticWallets > 0, "automatic_wallet_baselines_missing"},
		{n.BaselinePending > 0, "baseline_pending"},
		{n.BaselineNeedsAttention > 0, "baseline_needs_attention"},
	} {
		if check.failed {
			blockers = append(blockers, check.code)
		}
	}
	return blockers
}
