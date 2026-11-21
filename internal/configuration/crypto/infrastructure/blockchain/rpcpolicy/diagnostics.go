package rpcpolicy

import (
	"metapus/internal/configuration/crypto/domain/crypto"
)

// SafeReason only exposes known categories, never upstream errors or URLs.
func SafeReason(reason string) string {
	switch reason {
	case "credentials_expired", "query_limit", "timeout", "rate_limited", "unsupported", "archive_unavailable", "pool_exhausted", "pool_unavailable", "pool_key_missing", "invalid_credentials", "authentication", "access_denied", "head_lagging", "head_behind", "stale_head", "invalid_header", "invalid_response", "invalid_trace", "unsupported_trace", "response_limit", "http_error", "rpc_error", "transport_or_decode", "configuration_unavailable", "wrong_chain", "account_attempt_budget", "decoder_version_conflict", "admission_busy", "budget_unavailable", "rpc_unavailable", "invalid_request", "canceled", "not_found":
		return reason
	case "":
		return ""
	default:
		return "unknown"
	}
}

func (c *Circuit) Diagnostic(operation string) crypto.RuntimeRPCOperationStatus {
	status := crypto.RuntimeRPCOperationStatus{Operation: operation, State: "closed", Failures: c.Failures, ReasonCode: SafeReason(c.LastOutcome)}
	if status.ReasonCode == "" {
		status.ReasonCode = SafeReason(c.LastError)
	}
	if c.Failures > 0 {
		status.State = "degraded"
	}
	if !c.OpenUntil.IsZero() {
		until := c.OpenUntil
		status.RetryAt = &until
		status.State = "open"
	}
	if c.Probing {
		status.State = "half_open"
	}
	if c.Unsupported {
		status.State = "invalid"
		status.ReasonCode = "unsupported"
	}
	return status
}

func AddDiagnostic(status *crypto.RuntimeEndpointStatus, operation crypto.RuntimeRPCOperationStatus) {
	status.Operations = append(status.Operations, operation)
	if operation.Failures > status.Failures {
		status.Failures = operation.Failures
		status.LastError = operation.ReasonCode
	}
	if status.State == "wrong_chain" || status.State == "invalid" || status.State == "lagging" {
		return
	}
	switch operation.State {
	case "invalid":
		status.State = "invalid"
		status.LastError = operation.ReasonCode
	case "open":
		status.State = "open"
		status.LastError = operation.ReasonCode
	case "half_open":
		if status.State != "open" {
			status.State = "half_open"
		}
	case "degraded":
		if status.State == "closed" {
			status.State = "degraded"
		}
	}
}
