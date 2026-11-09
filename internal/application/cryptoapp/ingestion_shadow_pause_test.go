package cryptoapp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/domain/crypto"
)

func TestShadowPauseClosesIncidentWithoutClaimingRecovery(t *testing.T) {
	now := time.Now().UTC()
	state := IngestionAlertState{StartedAt: now.Add(-time.Hour), Scopes: map[string]IngestionAlertScope{
		_shadowAlertScope: {Active: shadowLagObservation(90)[_shadowAlertScope]},
	}}
	paused := &crypto.IngestionCondition{Scope: _shadowAlertScope, WatcherKey: "tron:shadow", Reason: "operator paused"}
	AdvanceIngestionAlerts(&state, now, map[string]*crypto.IngestionCondition{_shadowAlertScope: paused}, DefaultIngestionAlertPolicy())
	require.Len(t, state.Pending, 1)
	require.False(t, state.Pending[0].Recovered)
	require.Equal(t, "operator_paused", state.Pending[0].Resolution)
	require.Nil(t, state.Scopes[_shadowAlertScope].Active)
	AdvanceIngestionAlerts(&state, now.Add(30*time.Second), map[string]*crypto.IngestionCondition{_shadowAlertScope: paused}, DefaultIngestionAlertPolicy())
	require.Len(t, state.Pending, 1, "manual pause must not produce repeated closure notifications")
	for seconds := 60; seconds <= 180; seconds += 30 {
		AdvanceIngestionAlerts(&state, now.Add(time.Duration(seconds)*time.Second), shadowLagObservation(90), DefaultIngestionAlertPolicy())
	}
	require.Len(t, state.Pending, 1, "resuming with old history still requires fresh sustained lag evidence")
}
