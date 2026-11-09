package cryptoapp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
)

func TestIngestionAlertsDebounceStartupRecoveryAndReplay(t *testing.T) {
	now := time.Now().UTC()
	policy := DefaultIngestionAlertPolicy()
	state := IngestionAlertState{}
	condition := &crypto.IngestionCondition{Scope: "sepolia/native", NetworkCode: "SEPOLIA", WatcherKey: "native_eth", Severity: "error", Reason: "checkpoint stalled"}
	bad := map[string]*crypto.IngestionCondition{condition.Scope: condition}
	AdvanceIngestionAlerts(&state, now, bad, policy)
	AdvanceIngestionAlerts(&state, now.Add(60*time.Second), bad, policy)
	require.Empty(t, state.Pending, "startup grace must suppress early messages, not change readiness")
	AdvanceIngestionAlerts(&state, now.Add(90*time.Second), bad, policy)
	require.Len(t, state.Pending, 1)
	eventID := state.Pending[0].ID
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	var restarted IngestionAlertState
	require.NoError(t, json.Unmarshal(encoded, &restarted))
	AdvanceIngestionAlerts(&restarted, now.Add(120*time.Second), bad, policy)
	require.Len(t, restarted.Pending, 1)
	require.Equal(t, eventID, restarted.Pending[0].ID)
	healthy := map[string]*crypto.IngestionCondition{condition.Scope: nil}
	AdvanceIngestionAlerts(&restarted, now.Add(150*time.Second), healthy, policy)
	AdvanceIngestionAlerts(&restarted, now.Add(180*time.Second), nil, policy)
	AdvanceIngestionAlerts(&restarted, now.Add(210*time.Second), healthy, policy)
	require.Len(t, restarted.Pending, 1, "unknown evidence must interrupt recovery")
	AdvanceIngestionAlerts(&restarted, now.Add(240*time.Second), healthy, policy)
	AdvanceIngestionAlerts(&restarted, now.Add(270*time.Second), healthy, policy)
	require.Len(t, restarted.Pending, 2)
	require.True(t, restarted.Pending[1].Recovered)
	AdvanceIngestionAlerts(&restarted, now.Add(300*time.Second), healthy, policy)
	require.Len(t, restarted.Pending, 2, "healthy checks must not repeat a recovery")
}

func TestIngestionAlertsDoNotCountUnknownOrDowntimeAsContinuousFault(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	policy := DefaultIngestionAlertPolicy()
	condition := &crypto.IngestionCondition{Scope: "network", Severity: "error", Reason: "rpc_coverage"}
	bad := map[string]*crypto.IngestionCondition{condition.Scope: condition}
	AdvanceIngestionAlerts(&state, now, bad, policy)
	AdvanceIngestionAlerts(&state, now.Add(30*time.Second), nil, policy)
	AdvanceIngestionAlerts(&state, now.Add(60*time.Second), bad, policy)
	require.Empty(t, state.Pending)
	AdvanceIngestionAlerts(&state, now.Add(90*time.Second), bad, policy)
	AdvanceIngestionAlerts(&state, now.Add(5*time.Minute), bad, policy)
	require.Empty(t, state.Pending, "restart gap cannot fill the debounce duration")
	AdvanceIngestionAlerts(&state, now.Add(330*time.Second), bad, policy)
	AdvanceIngestionAlerts(&state, now.Add(360*time.Second), bad, policy)
	require.Len(t, state.Pending, 1)
}

func TestTRONStalledProgressDoesNotWaitAnotherMinute(t *testing.T) {
	now := time.Now().UTC()
	condition := &crypto.IngestionCondition{Scope: "tron/solidified", WatcherKey: "tron:solidified", Severity: "error", Reason: "checkpoint stalled"}
	observed := map[string]*crypto.IngestionCondition{condition.Scope: condition}
	policy := DefaultIngestionAlertPolicy()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	AdvanceIngestionAlerts(&state, now, observed, policy)
	require.Len(t, state.Pending, 1, "the collector already proved 60 seconds without progress")
	starting := IngestionAlertState{StartedAt: now}
	AdvanceIngestionAlerts(&starting, now, observed, policy)
	require.Empty(t, starting.Pending, "startup grace remains mandatory")
}

func TestIngestionAlertsEscalateOnceAndRequireSustainedHealthyRecovery(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	policy := DefaultIngestionAlertPolicy()
	condition := crypto.IngestionCondition{Scope: "native", Severity: "warning", Reason: "backlog"}
	observe := func(at time.Duration) {
		AdvanceIngestionAlerts(&state, now.Add(at), map[string]*crypto.IngestionCondition{condition.Scope: &condition}, policy)
	}
	observe(0)
	observe(30 * time.Second)
	observe(60 * time.Second)
	require.Len(t, state.Pending, 1)
	condition.Severity, condition.Reason = "error", "checkpoint stalled"
	observe(90 * time.Second)
	observe(120 * time.Second)
	observe(150 * time.Second)
	observe(180 * time.Second)
	require.Len(t, state.Pending, 2)
	require.Equal(t, "error", state.Pending[1].Condition.Severity)
	healthy := map[string]*crypto.IngestionCondition{condition.Scope: nil}
	AdvanceIngestionAlerts(&state, now.Add(210*time.Second), healthy, policy)
	observe(240 * time.Second)
	require.NotNil(t, state.Scopes[condition.Scope].Active)
	require.Len(t, state.Pending, 2)
}
