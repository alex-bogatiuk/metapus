package crypto_worker

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
)

type ingestionRecoveryProber struct {
	crypto.NetworkDriver
	calls    int
	deadline time.Time
	ctx      context.Context
}

func TestShadowAlertEvidencePreservesLagWithoutChangingDiagnosticState(t *testing.T) {
	tests := []struct {
		name   string
		give   IngestionStreamReport
		known  bool
		reason string
	}{
		{name: "healthy shadow", give: IngestionStreamReport{WatcherKey: "tron:shadow", Optional: true, State: "ready", LagSeconds: 0, ProgressAgeSeconds: 1}, known: true, reason: "backlog"},
		{name: "small moving lag", give: IngestionStreamReport{WatcherKey: "tron:shadow", Optional: true, State: "catching_up", Reason: "backlog", LagSeconds: 9, ProgressAgeSeconds: 4}, known: true, reason: "backlog"},
		{name: "significant lag", give: IngestionStreamReport{WatcherKey: "tron:shadow", Optional: true, State: "catching_up", Reason: "backlog", LagSeconds: 90, ProgressAgeSeconds: 4}, known: true, reason: "backlog"},
		{name: "unknown lag", give: IngestionStreamReport{WatcherKey: "tron:shadow", Optional: true, State: "catching_up", Reason: "backlog", LagSeconds: -1, ProgressAgeSeconds: -1}},
		{name: "RPC failure", give: IngestionStreamReport{WatcherKey: "tron:shadow", Optional: true, State: "degraded", Reason: "ingestion RPC coverage unavailable", LagSeconds: -1, ProgressAgeSeconds: -1}, known: true, reason: "ingestion RPC coverage unavailable"},
		{name: "stopped shadow", give: IngestionStreamReport{WatcherKey: "tron:shadow", Optional: true, State: "degraded", Reason: "checkpoint stalled", LagSeconds: 90, ProgressAgeSeconds: 61}, known: true, reason: "checkpoint stalled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := IngestionMonitorSnapshot{IngestionPreflightResult: IngestionPreflightResult{Ready: true, Networks: []IngestionNetworkReport{{
				NetworkCode: "TRON-NILE", State: "ready", Head: &crypto.IngestionHead{}, Streams: []IngestionStreamReport{tt.give},
			}}}}
			condition, known := IngestionConditions(snapshot)["network/TRON-NILE/tron:shadow"]
			require.Equal(t, tt.known, known)
			if known {
				require.NotNil(t, condition)
				require.Equal(t, tt.reason, condition.Reason)
				if tt.give.LagSeconds >= 0 {
					require.NotNil(t, condition.LagSeconds)
					require.Equal(t, tt.give.LagSeconds, *condition.LagSeconds)
					require.Equal(t, tt.give.ProgressAgeSeconds, *condition.ProgressAgeSeconds)
				} else {
					require.Nil(t, condition.LagSeconds)
					require.Nil(t, condition.ProgressAgeSeconds)
				}
			}
			require.Equal(t, tt.give, snapshot.Networks[0].Streams[0])
			require.True(t, snapshot.Ready, "alert classification must not alter readiness")
		})
	}
}

func TestShadowAlertPolicyReplaysObservedIncident(t *testing.T) {
	data, err := os.ReadFile("testdata/tron_shadow_lag_20261021.json")
	require.NoError(t, err)
	var samples []struct {
		At                 time.Time `json:"at"`
		State              string    `json:"state"`
		LagBlocks          int64     `json:"lagBlocks"`
		LagSeconds         int64     `json:"lagSeconds"`
		ProgressAgeSeconds int64     `json:"progressAgeSeconds"`
	}
	require.NoError(t, json.Unmarshal(data, &samples))
	require.Len(t, samples, 26)
	state := cryptoapp.IngestionAlertState{StartedAt: samples[0].At.Add(-time.Hour)}
	policy := cryptoapp.DefaultIngestionAlertPolicy()
	catchingUp := 0
	for _, sample := range samples {
		stream := IngestionStreamReport{WatcherKey: "tron:shadow", Optional: true, State: sample.State,
			LagBlocks: sample.LagBlocks, LagSeconds: sample.LagSeconds, ProgressAgeSeconds: sample.ProgressAgeSeconds, LeaseActive: true}
		if sample.State == "catching_up" {
			stream.Reason = "backlog"
			catchingUp++
		}
		snapshot := IngestionMonitorSnapshot{IngestionPreflightResult: IngestionPreflightResult{Ready: true, Networks: []IngestionNetworkReport{{
			NetworkCode: "TRON-NILE", State: "ready", Head: &crypto.IngestionHead{}, Streams: []IngestionStreamReport{stream},
		}}}}
		cryptoapp.AdvanceIngestionAlerts(&state, sample.At, IngestionConditions(snapshot), policy)
		require.Equal(t, stream, snapshot.Networks[0].Streams[0])
	}
	require.Equal(t, 14, catchingUp, "fixture includes the diagnostic states that previously caused notifications")
	require.Empty(t, state.Pending, "the observed 09:52–10:01 incident must not produce a warning or false recovery")
}

func (p *ingestionRecoveryProber) ProbeRuntime(ctx context.Context) {
	p.calls++
	p.deadline, _ = ctx.Deadline()
	p.ctx = ctx
}

func TestIngestionRecoveryProbeUsesBoundedContextAndHonorsShutdown(t *testing.T) {
	prober := &ingestionRecoveryProber{}
	probeIngestionRecovery(t.Context(), prober)
	require.Equal(t, 1, prober.calls)
	require.WithinDuration(t, time.Now().Add(5*time.Second), prober.deadline, time.Second)
	require.ErrorIs(t, prober.ctx.Err(), context.Canceled, "probe context released after read")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	probeIngestionRecovery(ctx, prober)
	require.Equal(t, 1, prober.calls, "shutdown must not start another RPC probe")
}

func TestIngestionConditionsKeepUnknownStreamsUnresolvedAndDetectInbox(t *testing.T) {
	key := "network/SEPOLIA/native_eth"
	result := IngestionConditions(IngestionMonitorSnapshot{ReadError: true})
	require.Equal(t, "read_failure", result["monitor"].Reason)
	_, known := result[key]
	require.False(t, known)
	snapshot := IngestionMonitorSnapshot{IngestionPreflightResult: IngestionPreflightResult{Networks: []IngestionNetworkReport{{NetworkCode: "SEPOLIA", State: "degraded"}}}, Inbox: map[string]crypto.IngestionInboxBacklog{"SEPOLIA": {Pending: 1, OldestAgeSeconds: 90}}}
	result = IngestionConditions(snapshot)
	require.Equal(t, "rpc_coverage", result["network/SEPOLIA"].Reason)
	require.Equal(t, "inbox_stalled", result["inbox/SEPOLIA"].Reason)
	_, known = result[key]
	require.False(t, known, "head failure is not stream recovery")
	snapshot.Networks[0].Head = &crypto.IngestionHead{ObservedAt: time.Now()}
	snapshot.Networks[0].Streams = []IngestionStreamReport{{WatcherKey: "native_eth", State: "ready"}}
	snapshot.Inbox["SEPOLIA"] = crypto.IngestionInboxBacklog{}
	result = IngestionConditions(snapshot)
	value, known := result[key]
	require.True(t, known)
	require.Equal(t, "backlog", value.Reason)
	require.EqualValues(t, 0, *value.LagSeconds)
	require.Nil(t, result["inbox/SEPOLIA"])
	snapshot.Inbox["SEPOLIA"] = crypto.IngestionInboxBacklog{NeedsReview: 1}
	require.Equal(t, "needs_review", IngestionConditions(snapshot)["inbox/SEPOLIA"].Reason)
}

func TestOptionalTRONReadFailureDoesNotBecomeFinancialSeverity(t *testing.T) {
	for _, key := range []string{"tron:latest", "tron:shadow", "tron:solidified"} {
		snapshot := IngestionMonitorSnapshot{IngestionPreflightResult: IngestionPreflightResult{Networks: []IngestionNetworkReport{{NetworkCode: "TRON", State: "ready", Head: &crypto.IngestionHead{}, Cutover: &crypto.IngestionCutover{LastErrorStream: key, LastErrorCode: "quorum_unavailable"}}}}}
		condition := IngestionConditions(snapshot)["network/TRON/block_evidence"]
		if key == "tron:solidified" {
			require.Equal(t, "error", condition.Severity)
		} else {
			require.Equal(t, "warning", condition.Severity)
		}
	}
}

func TestPausedShadowIsExplicitAndCannotHideFinancialOrInboxFaults(t *testing.T) {
	snapshot := IngestionMonitorSnapshot{IngestionPreflightResult: IngestionPreflightResult{Networks: []IngestionNetworkReport{{
		NetworkCode: "TRON-NILE", State: "paused", Cutover: &crypto.IngestionCutover{Mode: "shadow", ShadowPaused: true, LastErrorCode: "quorum_unavailable", LastErrorStream: "tron:shadow"},
	}}}, Inbox: map[string]crypto.IngestionInboxBacklog{"TRON-NILE": {NeedsReview: 1}}}
	conditions := IngestionConditions(snapshot)
	for _, scope := range []string{"network/TRON-NILE", "network/TRON-NILE/block_evidence", "network/TRON-NILE/tron:shadow"} {
		require.Equal(t, "operator paused", conditions[scope].Reason)
	}
	require.Equal(t, "needs_review", conditions["inbox/TRON-NILE"].Reason)
	now := time.Now()
	snapshot.Networks[0].Cutover.Mode = "hybrid"
	snapshot.Networks[0].Cutover.ActivatedAt = &now
	snapshot.Networks[0].Head = &crypto.IngestionHead{}
	snapshot.Networks[0].Streams = []IngestionStreamReport{{WatcherKey: "tron:solidified", State: "degraded", Reason: "checkpoint stalled"}}
	conditions = IngestionConditions(snapshot)
	require.Equal(t, "checkpoint stalled", conditions["network/TRON-NILE/tron:solidified"].Reason)
}

func TestConfiguredAlertLagUsesMeasurementsEvenWhenDiagnosticIsReady(t *testing.T) {
	now := time.Now().UTC()
	snapshot := IngestionMonitorSnapshot{IngestionPreflightResult: IngestionPreflightResult{Ready: true, Networks: []IngestionNetworkReport{{NetworkCode: "SEPOLIA", State: "ready", Head: &crypto.IngestionHead{}, Streams: []IngestionStreamReport{{WatcherKey: "native_eth", State: "ready", LagSeconds: 45, ProgressAgeSeconds: 1}}}}}}
	policy := cryptoapp.DefaultIngestionAlertPolicy()
	policy.NetworkSettings = map[string]rpcconfig.IngestionAlertSettings{"SEPOLIA": {MaxLagSeconds: 40, RecoveryLagSeconds: 20, FireAfterSeconds: 30}}
	state := cryptoapp.IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	cryptoapp.AdvanceIngestionIncidents(&state, now, IngestionConditions(snapshot), policy)
	cryptoapp.AdvanceIngestionIncidents(&state, now.Add(30*time.Second), IngestionConditions(snapshot), policy)
	require.Len(t, state.Pending, 1)
	require.Equal(t, int64(45), *state.Pending[0].Condition.LagSeconds)
	require.True(t, snapshot.Ready, "notification tuning cannot alter financial readiness")
	snapshot.Networks[0].Streams[0].LagSeconds = -1
	for seconds := 60; seconds <= 240; seconds += 30 {
		cryptoapp.AdvanceIngestionIncidents(&state, now.Add(time.Duration(seconds)*time.Second), IngestionConditions(snapshot), policy)
	}
	require.Len(t, state.Pending, 1, "unknown measurements do not recover the open incident")
}

func TestTRONLatestReadySamplesStillRespectConfiguredRecoveryLag(t *testing.T) {
	now := time.Now().UTC()
	policy := cryptoapp.DefaultIngestionAlertPolicy()
	policy.NetworkSettings = map[string]rpcconfig.IngestionAlertSettings{"TRON": {MaxLagSeconds: 60, RecoveryLagSeconds: 2, FireAfterSeconds: 30, RecoverAfterSeconds: 30}}
	snapshot := IngestionMonitorSnapshot{IngestionPreflightResult: IngestionPreflightResult{Ready: true, Networks: []IngestionNetworkReport{{NetworkCode: "TRON", State: "ready", Head: &crypto.IngestionHead{}, Streams: []IngestionStreamReport{{WatcherKey: "tron:latest", Optional: true, State: "catching_up", LagSeconds: 90}}}}}}
	state := cryptoapp.IngestionAlertState{StartedAt: now.Add(-time.Hour)}
	for seconds := 0; seconds <= 30; seconds += 30 {
		cryptoapp.AdvanceIngestionIncidents(&state, now.Add(time.Duration(seconds)*time.Second), IngestionConditions(snapshot), policy)
	}
	require.Len(t, state.Pending, 1)
	snapshot.Networks[0].Streams[0].State = "ready"
	snapshot.Networks[0].Streams[0].LagSeconds = 3
	for seconds := 60; seconds <= 180; seconds += 30 {
		cryptoapp.AdvanceIngestionIncidents(&state, now.Add(time.Duration(seconds)*time.Second), IngestionConditions(snapshot), policy)
	}
	require.Len(t, state.Pending, 1, "diagnostic ready is not proof of lag below the configured 2-second recovery threshold")
	snapshot.Networks[0].Streams[0].LagSeconds = 1
	cryptoapp.AdvanceIngestionIncidents(&state, now.Add(210*time.Second), IngestionConditions(snapshot), policy)
	cryptoapp.AdvanceIngestionIncidents(&state, now.Add(240*time.Second), IngestionConditions(snapshot), policy)
	require.Len(t, state.Pending, 2)
	require.True(t, state.Pending[1].Recovered)
}
