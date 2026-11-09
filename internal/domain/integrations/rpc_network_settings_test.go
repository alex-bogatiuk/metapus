package integrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRPCNetworkSettingsProtectBudgetsAndRecoveryBand(t *testing.T) {
	tests := []struct {
		name, family string
		give         RPCNetworkSettings
		wantError    bool
	}{
		{name: "EVM automatic", family: "evm"},
		{name: "TRON automatic", family: "tron"},
		{name: "reserve budget exhausted", family: "evm", give: RPCNetworkSettings{EVM: &EVMSettings{AttemptTimeoutMS: 10000, ReadBudgetMS: 15000}}, wantError: true},
		{name: "logs exceed operation", family: "evm", give: RPCNetworkSettings{EVM: &EVMSettings{LogsTimeoutMS: 6000}}, wantError: true},
		{name: "no recovery band", family: "tron", give: RPCNetworkSettings{Alerts: IngestionAlertSettings{RecoveryLagSeconds: 60}}, wantError: true},
		{name: "negative is not automatic", family: "tron", give: RPCNetworkSettings{Alerts: IngestionAlertSettings{FireAfterSeconds: -1}}, wantError: true},
		{name: "bulk consumes urgent capacity", family: "tron", give: RPCNetworkSettings{TRON: &TRONSettings{BackfillConcurrency: 8}}, wantError: true},
		{name: "insufficient buffer", family: "evm", give: RPCNetworkSettings{EVM: &EVMSettings{MaxBufferedBytes: 8 << 20}}, wantError: true},
		{name: "wrong family", family: "evm", give: RPCNetworkSettings{TRON: &TRONSettings{}}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.give.Validate(tt.family)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRPCSettingsDefaultsDoNotMutateRawAutomaticValuesOrChangeIngestionMode(t *testing.T) {
	raw := RPCNetworkSettings{TRON: &TRONSettings{IngestionMode: "solidified"}}
	effective := raw.Defaults("tron")
	require.Zero(t, raw.TRON.ProviderConcurrency)
	require.Equal(t, 8, effective.TRON.ProviderConcurrency)
	require.Equal(t, "solidified", effective.TRON.IngestionMode)
	view := planFixture()
	view.Family, view.Settings = "tron", raw
	_, err := view.Plan(RPCConfigurationUpdate{ExpectedRevision: view.Revision, Settings: &RPCNetworkSettings{TRON: &TRONSettings{IngestionMode: "hybrid"}}})
	require.Error(t, err)
}
