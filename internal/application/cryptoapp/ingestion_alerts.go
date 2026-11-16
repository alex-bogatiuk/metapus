package cryptoapp

import (
	"slices"
	"strings"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/rpcconfig"
)

type IngestionAlertState struct {
	NetworkSettings map[string]rpcconfig.IngestionAlertSettings `json:"networkSettings,omitempty"`
	StartedAt       time.Time                                   `json:"startedAt"`
	Scopes          map[string]IngestionAlertScope              `json:"scopes"`
	Pending         []crypto.IngestionAlertTransition           `json:"pending"`
}

type IngestionAlertScope struct {
	Active       *crypto.IngestionCondition `json:"active,omitempty"`
	Candidate    *crypto.IngestionCondition `json:"candidate,omitempty"`
	Since        time.Time                  `json:"since"`
	HealthySince time.Time                  `json:"healthySince,omitempty"`
	LastObserved time.Time                  `json:"lastObserved"`
}

type IngestionAlertPolicy struct {
	NetworkSettings map[string]rpcconfig.IngestionAlertSettings
	StartupGrace    time.Duration
	FireAfter       time.Duration
	RecoverAfter    time.Duration
	MaxSampleGap    time.Duration
	ShadowBacklog   IngestionBacklogAlertPolicy
	NetworkBacklog  IngestionBacklogAlertPolicy
}

type IngestionBacklogAlertPolicy struct {
	FireLagSeconds    int64
	RecoverLagSeconds int64
	FireAfter         time.Duration
	RecoverAfter      time.Duration
}

func DefaultIngestionAlertPolicy() IngestionAlertPolicy {
	return IngestionAlertPolicy{
		StartupGrace: 90 * time.Second, FireAfter: 60 * time.Second,
		RecoverAfter: 60 * time.Second, MaxSampleGap: 90 * time.Second,
		ShadowBacklog: IngestionBacklogAlertPolicy{
			FireLagSeconds: 60, RecoverLagSeconds: 30,
			FireAfter: 3 * time.Minute, RecoverAfter: 3 * time.Minute,
		},
		NetworkBacklog: IngestionBacklogAlertPolicy{
			FireLagSeconds: 60, RecoverLagSeconds: 30,
			FireAfter: time.Minute, RecoverAfter: 2 * time.Minute,
		},
	}
}

// AdvanceIngestionAlerts debounces evidence, preserves active incidents across
// unknown observations and queues durable, replayable notification transitions.
// observed includes healthy scopes; an absent scope is unknown, not recovered.
func AdvanceIngestionAlerts(state *IngestionAlertState, now time.Time, observed map[string]*crypto.IngestionCondition, policy IngestionAlertPolicy) {
	if state.StartedAt.IsZero() {
		state.StartedAt = now
	}
	if state.Scopes == nil {
		state.Scopes = make(map[string]IngestionAlertScope)
	}
	// Policy changes start a fresh continuous-evidence window while preserving
	// open incidents and pending delivery IDs.
	for network, settings := range policy.NetworkSettings {
		settings = settings.Defaults()
		old, exists := state.NetworkSettings[network]
		if !exists {
			old = (rpcconfig.IngestionAlertSettings{}).Defaults()
		}
		if old != settings {
			for key, scope := range state.Scopes {
				if ingestionScopeNetwork(key) == network {
					scope.Candidate, scope.HealthySince = nil, time.Time{}
					state.Scopes[key] = scope
				}
			}
		}
	}
	if len(policy.NetworkSettings) > 0 {
		state.NetworkSettings = make(map[string]rpcconfig.IngestionAlertSettings, len(policy.NetworkSettings))
		for network, settings := range policy.NetworkSettings {
			state.NetworkSettings[network] = settings.Defaults()
		}
	}
	for key, scope := range state.Scopes {
		if _, known := observed[key]; !known {
			scope.Candidate, scope.HealthySince = nil, time.Time{}
			state.Scopes[key] = scope
		}
	}
	keys := make([]string, 0, len(observed))
	for key := range observed {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		condition := observed[key]
		scope := state.Scopes[key]
		scopePolicy := policy.forScope(key)
		// Downtime cannot count as continuous fault/recovery evidence.
		if !scope.LastObserved.IsZero() && (now.Before(scope.LastObserved) || now.Sub(scope.LastObserved) > scopePolicy.MaxSampleGap) {
			scope.Candidate, scope.HealthySince = nil, time.Time{}
		}
		scope.LastObserved = now
		if condition != nil && condition.WatcherKey == "tron:shadow" && condition.Reason == "operator paused" {
			if scope.Active != nil {
				closed := *scope.Active
				closed.WatcherKey = "tron:shadow"
				queueIngestionTransition(state, closed, now, false)
				state.Pending[len(state.Pending)-1].Resolution = "operator_paused"
			}
			scope.Active, scope.Candidate, scope.HealthySince = nil, nil, time.Time{}
			state.Scopes[key] = scope
			continue
		}
		backlog, measuredBacklog := ingestionBacklogPolicy(condition, scopePolicy)
		if measuredBacklog && condition.LagSeconds == nil {
			scope.Candidate, scope.HealthySince = nil, time.Time{}
			state.Scopes[key] = scope
			continue
		}
		if measuredBacklog && condition.LagSeconds != nil {
			lag := *condition.LagSeconds
			switch {
			case lag < 0:
				// An unknown measurement cannot open or recover an incident.
				scope.Candidate, scope.HealthySince = nil, time.Time{}
				state.Scopes[key] = scope
				continue
			case lag > backlog.FireLagSeconds:
				// Sustained significant backlog is alertable below.
			case lag < backlog.RecoverLagSeconds:
				if scope.Active != nil {
					active := *scope.Active
					active.LagSeconds, active.ProgressAgeSeconds = condition.LagSeconds, condition.ProgressAgeSeconds
					scope.Active = &active
				}
				condition = nil
			default:
				// The measured middle band preserves the active incident but
				// interrupts both continuous fault and recovery evidence.
				scope.Candidate, scope.HealthySince = nil, time.Time{}
				state.Scopes[key] = scope
				continue
			}
		}
		if condition == nil {
			scope.Candidate = nil
			if scope.Active != nil {
				if scope.HealthySince.IsZero() {
					scope.HealthySince = now
				}
				recoverAfter := scopePolicy.RecoverAfter
				if isNetworkIncident(scope.Active) {
					recoverAfter = scopePolicy.NetworkBacklog.RecoverAfter
				} else if activePolicy, ok := ingestionBacklogPolicy(scope.Active, scopePolicy); ok {
					recoverAfter = activePolicy.RecoverAfter
				}
				if now.Sub(scope.HealthySince) >= recoverAfter {
					queueIngestionTransition(state, *scope.Active, now, true)
					scope.Active, scope.HealthySince = nil, time.Time{}
				}
			}
		} else {
			scope.HealthySince = time.Time{}
			if scope.Active != nil && scope.Active.Severity == condition.Severity && (scope.Active.Reason == condition.Reason || isNetworkIncident(condition)) {
				copyCondition := cloneIngestionCondition(*condition)
				scope.Active = &copyCondition
				scope.Candidate = nil // no repeated messages for an unchanged fault
			} else {
				if scope.Candidate == nil || scope.Candidate.Severity != condition.Severity || scope.Candidate.Reason != condition.Reason ||
					(measuredBacklog && condition.LagSeconds != nil && scope.Candidate.LagSeconds == nil) {
					// A persisted candidate from the old block-count policy did
					// not establish continuous lag above the new time threshold.
					copyCondition := cloneIngestionCondition(*condition)
					scope.Candidate, scope.Since = &copyCondition, now
				}
				fireAfter := scopePolicy.FireAfter
				if measuredBacklog {
					fireAfter = backlog.FireAfter
				}
				// The TRON checkpoint condition already requires 60 seconds
				// without durable progress. Do not debounce that evidence twice.
				if strings.HasPrefix(condition.WatcherKey, "tron:") && condition.Reason == "checkpoint stalled" {
					fireAfter = max(0, fireAfter-time.Minute)
				}
				if now.Sub(scope.Since) >= fireAfter && (scope.Active != nil || now.Sub(state.StartedAt) >= scopePolicy.StartupGrace) {
					copyCondition := cloneIngestionCondition(*condition)
					scope.Active, scope.Candidate = &copyCondition, nil
					queueIngestionTransition(state, copyCondition, now, false)
				}
			}
		}
		state.Scopes[key] = scope
	}
}

func isShadowBacklog(condition *crypto.IngestionCondition) bool {
	return condition != nil && condition.WatcherKey == "tron:shadow" && condition.Reason == "backlog"
}

func ingestionBacklogPolicy(condition *crypto.IngestionCondition, policy IngestionAlertPolicy) (IngestionBacklogAlertPolicy, bool) {
	if isShadowBacklog(condition) {
		return policy.ShadowBacklog, true
	}
	if condition != nil && condition.Reason == "backlog" && (isNetworkIncident(condition) || condition.WatcherKey == "tron:latest") {
		return policy.NetworkBacklog, true
	}
	return IngestionBacklogAlertPolicy{}, false
}

func queueIngestionTransition(state *IngestionAlertState, condition crypto.IngestionCondition, now time.Time, recovered bool) {
	state.Pending = append(state.Pending, crypto.IngestionAlertTransition{ID: id.New().String(), Condition: cloneIngestionCondition(condition), Recovered: recovered, At: now})
}

func cloneIngestionCondition(condition crypto.IngestionCondition) crypto.IngestionCondition {
	if condition.LagSeconds != nil {
		lag := *condition.LagSeconds
		condition.LagSeconds = &lag
	}
	if condition.ProgressAgeSeconds != nil {
		age := *condition.ProgressAgeSeconds
		condition.ProgressAgeSeconds = &age
	}
	return condition
}

func ingestionScopeNetwork(scope string) string {
	if network, ok := strings.CutPrefix(scope, "network/"); ok {
		code, _, _ := strings.Cut(network, "/")
		return code
	}
	if network, ok := strings.CutPrefix(scope, "inbox/"); ok {
		return network
	}
	return ""
}

func (p IngestionAlertPolicy) forScope(scope string) IngestionAlertPolicy {
	if settings, ok := p.NetworkSettings[ingestionScopeNetwork(scope)]; ok {
		settings = settings.Defaults()
		p.FireAfter = time.Duration(settings.FireAfterSeconds) * time.Second
		p.RecoverAfter = time.Duration(settings.RecoverAfterSeconds) * time.Second
		p.NetworkBacklog = IngestionBacklogAlertPolicy{FireLagSeconds: settings.MaxLagSeconds, RecoverLagSeconds: settings.RecoveryLagSeconds, FireAfter: p.FireAfter, RecoverAfter: p.RecoverAfter}
	}
	return p
}
