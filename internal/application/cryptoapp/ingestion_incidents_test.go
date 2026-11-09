package cryptoapp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/domain/crypto"
)

func TestNetworkIncidentKeepsPartialRecoveryOpenAndMeasuresWorstStream(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	policy := DefaultIngestionAlertPolicy()
	root := "network/TRON-NILE"
	lag, age := int64(1170), int64(1166)
	stream := &crypto.IngestionCondition{Scope: root + "/tron:solidified", NetworkCode: "TRON-NILE", WatcherKey: "tron:solidified", Severity: "error", Reason: "checkpoint stalled", LagSeconds: &lag, ProgressAgeSeconds: &age}
	observed := map[string]*crypto.IngestionCondition{root: nil, root + "/block_evidence": nil, stream.Scope: stream, root + "/tron:latest": nil}
	advance := func(seconds int) {
		AdvanceIngestionIncidents(&state, now.Add(time.Duration(seconds)*time.Second), observed, policy)
	}
	advance(0)
	require.Len(t, state.Pending, 1)
	observed[root+"/block_evidence"] = &crypto.IngestionCondition{Scope: root + "/block_evidence", NetworkCode: "TRON-NILE", WatcherKey: "tron:solidified", Severity: "error", Reason: "quorum_unavailable"}
	advance(30)
	advance(60)
	require.Len(t, state.Pending, 1, "quorum and stopped checkpoint belong to the same incident")
	observed[root+"/block_evidence"] = nil
	lag, age = 900, 2
	stream.Severity, stream.Reason = "warning", "backlog"
	advance(90)
	advance(120)
	advance(150)
	require.Len(t, state.Pending, 2)
	require.False(t, state.Pending[1].Recovered, "restored quorum with backlog is progress, not full recovery")
	require.Equal(t, int64(900), *state.Pending[1].Condition.LagSeconds)
	lag = 9
	for seconds := 180; seconds < 300; seconds += 30 {
		advance(seconds)
	}
	require.Len(t, state.Pending, 2)
	advance(300)
	require.Len(t, state.Pending, 3)
	require.True(t, state.Pending[2].Recovered)
	require.Equal(t, int64(9), *state.Pending[2].Condition.LagSeconds)
	require.Equal(t, int64(900), *state.Pending[1].Condition.LagSeconds, "queued evidence remains immutable")
}

func TestNetworkBacklogUsesWorstLagAndHysteresis(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	root := "network/SEPOLIA"
	lagA, lagB := int64(9), int64(90)
	observed := map[string]*crypto.IngestionCondition{
		root:                 nil,
		root + "/erc20":      {Scope: root + "/erc20", NetworkCode: "SEPOLIA", Severity: "warning", Reason: "backlog", LagSeconds: &lagA},
		root + "/native_eth": {Scope: root + "/native_eth", NetworkCode: "SEPOLIA", Severity: "warning", Reason: "backlog", LagSeconds: &lagB},
	}
	policy := DefaultIngestionAlertPolicy()
	for seconds := 0; seconds <= 60; seconds += 30 {
		AdvanceIngestionIncidents(&state, now.Add(time.Duration(seconds)*time.Second), observed, policy)
	}
	require.Len(t, state.Pending, 1)
	require.Equal(t, int64(90), *state.Pending[0].Condition.LagSeconds)
	lagB = 45
	for seconds := 90; seconds <= 300; seconds += 30 {
		AdvanceIngestionIncidents(&state, now.Add(time.Duration(seconds)*time.Second), observed, policy)
	}
	require.Len(t, state.Pending, 1, "middle-band lag cannot declare recovery")
	AdvanceIngestionIncidents(&state, now.Add(330*time.Second), nil, policy)
	require.NotNil(t, state.Scopes[root].Active, "missing network evidence is unknown")
}

func TestNetworkIncidentMigratesLegacyStateWithoutReplayingOrPrematureRecovery(t *testing.T) {
	now := time.Now().UTC()
	root := "network/SEPOLIA"
	oldKey := root + "/native_eth"
	condition := &crypto.IngestionCondition{Scope: oldKey, NetworkCode: "SEPOLIA", WatcherKey: "native_eth", Severity: "error", Reason: "checkpoint stalled"}
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour), Scopes: map[string]IngestionAlertScope{oldKey: {Active: condition, HealthySince: now.Add(-time.Hour), LastObserved: now.Add(-30 * time.Second)}}, Pending: []crypto.IngestionAlertTransition{{ID: "persisted-event", Condition: *condition}}}
	AdvanceIngestionIncidents(&state, now, map[string]*crypto.IngestionCondition{root: nil, oldKey: nil}, DefaultIngestionAlertPolicy())
	require.NotContains(t, state.Scopes, oldKey)
	require.NotNil(t, state.Scopes[root].Active)
	require.Equal(t, now, state.Scopes[root].HealthySince)
	require.Len(t, state.Pending, 1)
	require.Equal(t, "persisted-event", state.Pending[0].ID)
	require.Equal(t, oldKey, state.Pending[0].Condition.Scope, "pending event content and deduplication IDs survive migration")
}

func TestShadowCoverageAndEvidenceRemainOneOptionalIncident(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	root := "network/TRON-NILE"
	shadow := root + "/tron:shadow"
	observed := map[string]*crypto.IngestionCondition{
		root:                     {Scope: root, NetworkCode: "TRON-NILE", Severity: "error", Reason: "rpc_coverage"},
		root + "/block_evidence": {Scope: root + "/block_evidence", NetworkCode: "TRON-NILE", WatcherKey: "tron:shadow", Severity: "warning", Reason: "quorum_unavailable"},
		shadow:                   {Scope: shadow, NetworkCode: "TRON-NILE", WatcherKey: "tron:shadow", Severity: "warning", Reason: "ingestion RPC coverage unavailable"},
	}
	policy := DefaultIngestionAlertPolicy()
	for seconds := 0; seconds <= 60; seconds += 30 {
		AdvanceIngestionIncidents(&state, now.Add(time.Duration(seconds)*time.Second), observed, policy)
	}
	require.Len(t, state.Pending, 1)
	require.Equal(t, shadow, state.Pending[0].Condition.Scope)
	require.Equal(t, "warning", state.Pending[0].Condition.Severity)
	require.Equal(t, "tron:shadow", state.Pending[0].Condition.WatcherKey)
	for key := range observed {
		observed[key] = &crypto.IngestionCondition{Scope: key, NetworkCode: "TRON-NILE", WatcherKey: "tron:shadow", Reason: "operator paused", Severity: "info"}
	}
	AdvanceIngestionIncidents(&state, now.Add(90*time.Second), observed, policy)
	require.Len(t, state.Pending, 2)
	require.Equal(t, "operator_paused", state.Pending[1].Resolution)
	require.False(t, state.Pending[1].Recovered)
}
