package cryptoapp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/domain/crypto"
	"metapus/internal/domain/integrations"
)

func TestIngestionSettingsApplyIndependentlyAndResetContinuousEvidence(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	policy := DefaultIngestionAlertPolicy()
	settings := integrations.IngestionAlertSettings{MaxLagSeconds: 120, RecoveryLagSeconds: 20, FireAfterSeconds: 90, RecoverAfterSeconds: 60}
	policy.NetworkSettings = map[string]integrations.IngestionAlertSettings{"CUSTOM": settings}
	lag := int64(90)
	observed := map[string]*crypto.IngestionCondition{}
	for _, network := range []string{"CUSTOM", "DEFAULT"} {
		root := "network/" + network
		observed[root] = &crypto.IngestionCondition{Scope: root, NetworkCode: network, Severity: "warning", Reason: "backlog", LagSeconds: &lag}
	}
	advance := func(seconds int) {
		AdvanceIngestionIncidents(&state, now.Add(time.Duration(seconds)*time.Second), observed, policy)
	}
	for seconds := 0; seconds <= 60; seconds += 30 {
		advance(seconds)
	}
	require.Len(t, state.Pending, 1)
	require.Equal(t, "DEFAULT", state.Pending[0].Condition.NetworkCode)
	lag = 150
	advance(90)
	advance(120)
	// A changed delay must not retroactively use the previous 30 seconds.
	settings.FireAfterSeconds = 60
	policy.NetworkSettings["CUSTOM"] = settings
	advance(150)
	advance(180)
	require.Len(t, state.Pending, 1)
	advance(210)
	require.Len(t, state.Pending, 2)
	require.Equal(t, "CUSTOM", state.Pending[1].Condition.NetworkCode)
	lag = 25
	for seconds := 240; seconds <= 360; seconds += 30 {
		advance(seconds)
	}
	require.NotNil(t, state.Scopes["network/CUSTOM"].Active, "custom recovery needs lag below 20")
	lag = 10
	advance(390)
	advance(420)
	require.NotNil(t, state.Scopes["network/CUSTOM"].Active)
	advance(450)
	require.Nil(t, state.Scopes["network/CUSTOM"].Active)
	require.True(t, state.Pending[len(state.Pending)-1].Recovered)
}

func TestConfiguredCheckpointDelayCountsExistingStallEvidenceOnce(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	policy := DefaultIngestionAlertPolicy()
	policy.NetworkSettings = map[string]integrations.IngestionAlertSettings{"TRON": {FireAfterSeconds: 120}}
	condition := &crypto.IngestionCondition{Scope: "network/TRON/tron:solidified", NetworkCode: "TRON", WatcherKey: "tron:solidified", Severity: "error", Reason: "checkpoint stalled"}
	for seconds := 0; seconds < 60; seconds += 30 {
		AdvanceIngestionIncidents(&state, now.Add(time.Duration(seconds)*time.Second), map[string]*crypto.IngestionCondition{condition.Scope: condition}, policy)
	}
	require.Empty(t, state.Pending)
	AdvanceIngestionIncidents(&state, now.Add(time.Minute), map[string]*crypto.IngestionCondition{condition.Scope: condition}, policy)
	require.Len(t, state.Pending, 1)
}

func TestTRONLatestBacklogUsesMeasuredNetworkThresholds(t *testing.T) {
	now := time.Now().UTC()
	policy := DefaultIngestionAlertPolicy()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	key := "network/TRON-NILE/tron:latest"
	lag := int64(9)
	condition := &crypto.IngestionCondition{Scope: key, NetworkCode: "TRON-NILE", WatcherKey: "tron:latest", Severity: "warning", Reason: "backlog", LagSeconds: &lag}
	advance := func(seconds int) {
		AdvanceIngestionIncidents(&state, now.Add(time.Duration(seconds)*time.Second), map[string]*crypto.IngestionCondition{key: condition}, policy)
	}
	for seconds := 0; seconds <= 300; seconds += 30 {
		advance(seconds)
	}
	require.Empty(t, state.Pending, "ordinary 9-second latest lag must not open an incident")
	lag = 90
	for seconds := 330; seconds <= 390; seconds += 30 {
		advance(seconds)
	}
	require.Len(t, state.Pending, 1)
	lag = 45
	for seconds := 420; seconds <= 570; seconds += 30 {
		advance(seconds)
	}
	require.Len(t, state.Pending, 1, "middle-band samples cannot recover the optional stream")
	condition.LagSeconds = nil
	advance(600)
	require.Len(t, state.Pending, 1, "missing measurement cannot recover an active incident")
	condition.LagSeconds = &lag
	lag = 9
	for seconds := 630; seconds <= 750; seconds += 30 {
		advance(seconds)
	}
	require.Len(t, state.Pending, 2)
	require.True(t, state.Pending[1].Recovered)
}
