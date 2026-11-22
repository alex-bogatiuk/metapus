package cryptoapp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/json"
)

const _shadowAlertScope = "network/TRON-NILE/tron:shadow"

func shadowLagObservation(lag int64) map[string]*crypto.IngestionCondition {
	progressAge := int64(4)
	return map[string]*crypto.IngestionCondition{_shadowAlertScope: {
		Scope: _shadowAlertScope, NetworkCode: "TRON-NILE", WatcherKey: "tron:shadow",
		Severity: "warning", Reason: "backlog", LagSeconds: &lag, ProgressAgeSeconds: &progressAge,
	}}
}

func TestShadowBacklogIgnoresNormalMovingLag(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	policy := DefaultIngestionAlertPolicy()
	lags := []int64{3, 6, 9, 18, 21, 30}
	for i := range 1200 {
		AdvanceIngestionAlerts(&state, now.Add(time.Duration(i)*30*time.Second), shadowLagObservation(lags[i%len(lags)]), policy)
	}
	require.Empty(t, state.Pending, "hours of normal ready/catching_up oscillation must remain silent")
	require.Nil(t, state.Scopes[_shadowAlertScope].Active)
}

func TestShadowBacklogRequiresSustainedLagAndSeparateRecoveryThreshold(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	policy := DefaultIngestionAlertPolicy()
	observe := func(seconds int, lag int64) {
		AdvanceIngestionAlerts(&state, now.Add(time.Duration(seconds)*time.Second), shadowLagObservation(lag), policy)
	}
	for seconds := 0; seconds <= 180; seconds += 30 {
		observe(seconds, 60)
	}
	require.Empty(t, state.Pending, "the firing threshold is strictly above 60 seconds")
	for seconds := 210; seconds < 390; seconds += 30 {
		observe(seconds, 61)
	}
	require.Empty(t, state.Pending, "large lag must persist for three minutes")
	observe(390, 84)
	require.Len(t, state.Pending, 1)
	require.Equal(t, int64(84), *state.Pending[0].Condition.LagSeconds, "notification uses the firing sample")
	for seconds := 420; seconds <= 600; seconds += 30 {
		observe(seconds, 30)
	}
	require.Len(t, state.Pending, 1, "the middle band must preserve an active incident")
	for seconds := 630; seconds <= 720; seconds += 30 {
		observe(seconds, 29)
	}
	observe(750, 45)
	require.Zero(t, state.Scopes[_shadowAlertScope].HealthySince, "middle-band lag interrupts recovery")
	for seconds := 780; seconds < 960; seconds += 30 {
		observe(seconds, 29)
	}
	require.Len(t, state.Pending, 1)
	observe(960, 6)
	require.Len(t, state.Pending, 2)
	require.True(t, state.Pending[1].Recovered)
	require.Equal(t, int64(6), *state.Pending[1].Condition.LagSeconds)
	require.Equal(t, int64(84), *state.Pending[0].Condition.LagSeconds, "recovery must not mutate an already queued event")
	require.Nil(t, state.Scopes[_shadowAlertScope].Active)
}

func TestShadowBacklogMiddleBandInterruptsOpening(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	policy := DefaultIngestionAlertPolicy()
	for seconds := 0; seconds <= 90; seconds += 30 {
		AdvanceIngestionAlerts(&state, now.Add(time.Duration(seconds)*time.Second), shadowLagObservation(90), policy)
	}
	AdvanceIngestionAlerts(&state, now.Add(120*time.Second), shadowLagObservation(60), policy)
	for seconds := 150; seconds < 330; seconds += 30 {
		AdvanceIngestionAlerts(&state, now.Add(time.Duration(seconds)*time.Second), shadowLagObservation(90), policy)
	}
	require.Empty(t, state.Pending)
	AdvanceIngestionAlerts(&state, now.Add(330*time.Second), shadowLagObservation(90), policy)
	require.Len(t, state.Pending, 1)
}

func TestShadowBacklogUnknownSamplesInterruptTimersAndSurviveRestart(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	policy := DefaultIngestionAlertPolicy()
	observe := func(seconds int, observed map[string]*crypto.IngestionCondition) {
		AdvanceIngestionAlerts(&state, now.Add(time.Duration(seconds)*time.Second), observed, policy)
	}
	restart := func() {
		encoded, err := json.Marshal(state)
		require.NoError(t, err)
		var resumed IngestionAlertState
		require.NoError(t, json.Unmarshal(encoded, &resumed))
		state = resumed
	}
	for seconds := 0; seconds <= 90; seconds += 30 {
		observe(seconds, shadowLagObservation(90))
	}
	observe(120, nil)
	for seconds := 150; seconds <= 300; seconds += 30 {
		observe(seconds, shadowLagObservation(90))
	}
	require.Empty(t, state.Pending)
	restart()
	observe(330, shadowLagObservation(90))
	require.Len(t, state.Pending, 1)
	eventID := state.Pending[0].ID
	for seconds := 360; seconds <= 450; seconds += 30 {
		observe(seconds, shadowLagObservation(6))
	}
	observe(480, shadowLagObservation(-1))
	for seconds := 510; seconds <= 660; seconds += 30 {
		observe(seconds, shadowLagObservation(6))
	}
	require.Len(t, state.Pending, 1, "unknown lag must not resolve an active incident")
	restart()
	observe(690, shadowLagObservation(6))
	require.Len(t, state.Pending, 2)
	require.Equal(t, eventID, state.Pending[0].ID)
	require.True(t, state.Pending[1].Recovered)
}

func TestShadowBacklogDoesNotCountDowntimeAsEvidence(t *testing.T) {
	for _, active := range []bool{false, true} {
		name := "opening"
		if active {
			name = "recovery"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Now().UTC()
			state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
			policy := DefaultIngestionAlertPolicy()
			lag := int64(90)
			if active {
				state.Scopes = map[string]IngestionAlertScope{_shadowAlertScope: {Active: shadowLagObservation(90)[_shadowAlertScope]}}
				lag = 6
			}
			AdvanceIngestionAlerts(&state, now, shadowLagObservation(lag), policy)
			AdvanceIngestionAlerts(&state, now.Add(30*time.Second), shadowLagObservation(lag), policy)
			AdvanceIngestionAlerts(&state, now.Add(5*time.Minute), shadowLagObservation(lag), policy)
			require.Empty(t, state.Pending)
			for seconds := 330; seconds <= 480; seconds += 30 {
				AdvanceIngestionAlerts(&state, now.Add(time.Duration(seconds)*time.Second), shadowLagObservation(lag), policy)
			}
			require.Len(t, state.Pending, 1)
			require.Equal(t, active, state.Pending[0].Recovered)
		})
	}
}

func TestShadowBacklogUpgradeDoesNotReuseOldCandidateEvidence(t *testing.T) {
	now := time.Now().UTC()
	legacy := &crypto.IngestionCondition{Scope: _shadowAlertScope, WatcherKey: "tron:shadow", Severity: "warning", Reason: "backlog"}
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour), Scopes: map[string]IngestionAlertScope{
		_shadowAlertScope: {Candidate: legacy, Since: now.Add(-time.Hour), LastObserved: now.Add(-30 * time.Second)},
	}}
	policy := DefaultIngestionAlertPolicy()
	for seconds := 0; seconds < 180; seconds += 30 {
		AdvanceIngestionAlerts(&state, now.Add(time.Duration(seconds)*time.Second), shadowLagObservation(90), policy)
	}
	require.Empty(t, state.Pending, "old block-count evidence did not prove sustained significant time lag")
	AdvanceIngestionAlerts(&state, now.Add(180*time.Second), shadowLagObservation(90), policy)
	require.Len(t, state.Pending, 1)
	state = IngestionAlertState{StartedAt: now.Add(-time.Hour), Scopes: map[string]IngestionAlertScope{_shadowAlertScope: {Active: legacy}}}
	for seconds := 0; seconds <= 180; seconds += 30 {
		AdvanceIngestionAlerts(&state, now.Add(time.Duration(seconds)*time.Second), shadowLagObservation(6), policy)
	}
	require.Len(t, state.Pending, 1, "an existing incident still receives one sustained recovery")
	require.True(t, state.Pending[0].Recovered)
}

// Latest backlog is measured separately in TestTRONLatestBacklogUsesMeasuredNetworkThresholds.
func TestShadowPolicyPreservesOtherFaultSensitivity(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		reason    string
		immediate bool
	}{
		{name: "shadow stopped", key: "tron:shadow", reason: "checkpoint stalled", immediate: true},
		{name: "shadow RPC coverage", key: "tron:shadow", reason: "ingestion RPC coverage unavailable"},
		{name: "shadow quorum", key: "tron:shadow", reason: "quorum_unavailable"},
		{name: "shadow disagreement", key: "tron:shadow", reason: "provider_disagreement"},
		{name: "shadow incomplete receipts", key: "tron:shadow", reason: "incomplete_receipts"},
		{name: "financial solidified backlog", key: "tron:solidified", reason: "backlog"},
		{name: "EVM backlog", key: "native_eth", reason: "backlog"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().UTC()
			lag := int64(120)
			condition := &crypto.IngestionCondition{Scope: "scope", WatcherKey: tt.key, Severity: "warning", Reason: tt.reason, LagSeconds: &lag}
			state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
			policy := DefaultIngestionAlertPolicy()
			for seconds := 0; seconds <= 60; seconds += 30 {
				AdvanceIngestionAlerts(&state, now.Add(time.Duration(seconds)*time.Second), map[string]*crypto.IngestionCondition{condition.Scope: condition}, policy)
				if seconds < 60 && !tt.immediate {
					require.Empty(t, state.Pending)
				}
				if tt.immediate {
					require.Len(t, state.Pending, 1)
				}
			}
			require.Len(t, state.Pending, 1)
			AdvanceIngestionAlerts(&state, now.Add(90*time.Second), map[string]*crypto.IngestionCondition{condition.Scope: nil}, policy)
			AdvanceIngestionAlerts(&state, now.Add(120*time.Second), map[string]*crypto.IngestionCondition{condition.Scope: nil}, policy)
			AdvanceIngestionAlerts(&state, now.Add(150*time.Second), map[string]*crypto.IngestionCondition{condition.Scope: nil}, policy)
			require.Len(t, state.Pending, 2, "other incidents retain the one-minute recovery policy")
		})
	}
}

func TestShadowBacklogDoesNotDelayStalledCheckpointEscalation(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	policy := DefaultIngestionAlertPolicy()
	for seconds := 0; seconds <= 180; seconds += 30 {
		AdvanceIngestionAlerts(&state, now.Add(time.Duration(seconds)*time.Second), shadowLagObservation(90), policy)
	}
	require.Len(t, state.Pending, 1)
	stalled := shadowLagObservation(120)
	stalled[_shadowAlertScope].Reason = "checkpoint stalled"
	AdvanceIngestionAlerts(&state, now.Add(210*time.Second), stalled, policy)
	require.Len(t, state.Pending, 2)
	require.Equal(t, "checkpoint stalled", state.Pending[1].Condition.Reason)
}
