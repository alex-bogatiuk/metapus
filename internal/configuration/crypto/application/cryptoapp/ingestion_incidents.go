package cryptoapp

import (
	"slices"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
)

// AdvanceIngestionIncidents keeps mandatory coverage, evidence and streams in
// one network incident. Optional TRON streams and inbox review stay independent.
// Existing pending IDs survive the migration from per-check notifications.
func AdvanceIngestionIncidents(state *IngestionAlertState, now time.Time, observed map[string]*crypto.IngestionCondition, policy IngestionAlertPolicy) {
	grouped := make(map[string]*crypto.IngestionCondition, len(observed))
	keys := make([]string, 0, len(observed))
	optionalRoots := make(map[string]string)
	for key := range observed {
		keys = append(keys, key)
		if root, shadow := strings.CutSuffix(key, "/tron:shadow"); shadow {
			optionalRoots[root] = key
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		condition := observed[key]
		target := ingestionIncidentScope(key, condition)
		if optional, ok := optionalRoots[target]; ok {
			target = optional
		}
		if condition != nil {
			copyCondition := incidentCondition(*condition, target)
			condition = &copyCondition
		}
		current, known := grouped[target]
		if !known {
			grouped[target] = nil
		}
		if condition != nil && ingestionConditionWorse(condition, current) {
			copyCondition := *condition
			grouped[target] = &copyCondition
		}
	}
	if state.Scopes == nil {
		state.Scopes = make(map[string]IngestionAlertScope)
	}
	keys = keys[:0]
	for key := range state.Scopes {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		scope := state.Scopes[key]
		condition := scope.Active
		if condition == nil {
			condition = scope.Candidate
		}
		target := ingestionIncidentScope(key, condition)
		if optional, ok := optionalRoots[target]; ok {
			target = optional
		}
		if target == key {
			continue
		}
		destination := state.Scopes[target]
		if scope.Active != nil {
			copyCondition := incidentCondition(*scope.Active, target)
			if ingestionConditionWorse(&copyCondition, destination.Active) {
				destination.Active = &copyCondition
			}
		}
		// The old checks did not prove continuous health of the whole network.
		destination.Candidate, destination.HealthySince = nil, time.Time{}
		destination.LastObserved = maxTime(destination.LastObserved, scope.LastObserved)
		state.Scopes[target] = destination
		delete(state.Scopes, key)
	}
	AdvanceIngestionAlerts(state, now, grouped, policy)
}

func incidentCondition(condition crypto.IngestionCondition, scope string) crypto.IngestionCondition {
	condition = cloneIngestionCondition(condition)
	condition.Scope = scope
	if strings.HasSuffix(scope, "/tron:shadow") {
		condition.WatcherKey = "tron:shadow"
		if condition.Severity == "error" {
			condition.Severity = "warning"
		}
	}
	return condition
}

func ingestionIncidentScope(scope string, condition *crypto.IngestionCondition) string {
	if !strings.HasPrefix(scope, "network/") {
		return scope
	}
	network, stream, _ := strings.Cut(strings.TrimPrefix(scope, "network/"), "/")
	if stream == "tron:shadow" || stream == "tron:latest" {
		return scope
	}
	if condition != nil && condition.Reason != "operator paused" && (condition.WatcherKey == "tron:shadow" || condition.WatcherKey == "tron:latest") {
		return "network/" + network + "/" + condition.WatcherKey
	}
	return "network/" + network
}

func isNetworkIncident(condition *crypto.IngestionCondition) bool {
	return condition != nil && condition.NetworkCode != "" && condition.Scope == "network/"+condition.NetworkCode
}

func ingestionConditionRank(condition *crypto.IngestionCondition) int {
	rank := 0
	switch condition.Severity {
	case "error":
		rank = 30
	case "warning":
		rank = 20
	case "info":
		rank = 10
	}
	switch condition.Reason {
	case "provider_disagreement", "transfer_mismatch", "solidified_payment_changed":
		rank += 5
	case "checkpoint stalled":
		rank += 4
	case "quorum_unavailable":
		rank += 3
	case "rpc_coverage", "stale head", "ingestion RPC coverage unavailable":
		rank += 2
	case "backlog":
		rank++
	}
	return rank
}

func ingestionConditionWorse(condition, current *crypto.IngestionCondition) bool {
	if current == nil {
		return true
	}
	if a, b := ingestionConditionRank(condition), ingestionConditionRank(current); a != b {
		return a > b
	}
	return condition.LagSeconds != nil && (current.LagSeconds == nil || *condition.LagSeconds > *current.LagSeconds)
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
