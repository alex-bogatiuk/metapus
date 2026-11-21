package crypto

import (
	"context"
	"time"
)

// IngestionCondition is operational evidence. It never creates financial holds
// or changes watcher/payment state. One scope has at most one active condition.
type IngestionCondition struct {
	Scope       string `json:"scope"`
	NetworkCode string `json:"networkCode"`
	WatcherKey  string `json:"watcherKey,omitempty"`
	Severity    string `json:"severity"`
	Reason      string `json:"reason"`
	// Optional measurements distinguish observed zero lag from unavailable
	// evidence. Backlog samples include measured lag so the
	// alert policy can apply hysteresis independently of diagnostic readiness.
	LagSeconds         *int64 `json:"lagSeconds,omitempty"`
	ProgressAgeSeconds *int64 `json:"progressAgeSeconds,omitempty"`
}

type IngestionInboxBacklog struct {
	Pending          int64 `json:"pending"`
	Processing       int64 `json:"processing"`
	NeedsReview      int64 `json:"needsReview"`
	OldestAgeSeconds int64 `json:"oldestAgeSeconds"`
}

type IngestionAlertTransition struct {
	Resolution string             `json:"resolution,omitempty"`
	ID         string             `json:"id"`
	Condition  IngestionCondition `json:"condition"`
	Recovered  bool               `json:"recovered"`
	At         time.Time          `json:"at"`
}

type IngestionRPCCheckOptions struct {
	InternalTransferTxHash string // Optional public positive internal-TRX sample locator.
	Samples                int
	HistoricalBlock        int64
}

type IngestionRPCOperationCheck struct {
	Operation  string         `json:"operation"`
	Attempted  int            `json:"attempted"`
	Successful int            `json:"successful"`
	P50Millis  int64          `json:"p50Millis"`
	P95Millis  int64          `json:"p95Millis"`
	Errors     map[string]int `json:"errors"`
}

type IngestionRPCProviderCheck struct {
	ProviderID string                       `json:"providerId"`
	Role       string                       `json:"role,omitempty"`
	Ready      bool                         `json:"ready"`
	Operations []IngestionRPCOperationCheck `json:"operations"`
}

type IngestionRPCCheckResult struct {
	DecoderVersion int                         `json:"decoderVersion,omitempty"`
	Comparisons    []IngestionRPCComparison    `json:"comparisons,omitempty"`
	ObservedAt     time.Time                   `json:"observedAt"`
	NetworkCode    string                      `json:"networkCode"`
	Ready          bool                        `json:"ready"`
	Heights        []int64                     `json:"heights"`
	Providers      []IngestionRPCProviderCheck `json:"providers"`
	RoleCoverage   []IngestionRPCRoleCoverage  `json:"roleCoverage,omitempty"`
}

type IngestionRPCComparisonSource struct {
	ProviderID     string           `json:"providerId"`
	TrustGroup     string           `json:"trustGroup"`
	Digest         string           `json:"digest"`
	SourceDigest   string           `json:"sourceDigest,omitempty"`
	ExcludedTraces []TraceExclusion `json:"excludedTraces,omitempty"`
}

type IngestionRPCComparison struct {
	Role    string                         `json:"role"`
	Height  int64                          `json:"height"`
	Matched bool                           `json:"matched"`
	Sources []IngestionRPCComparisonSource `json:"sources"`
}

type IngestionRPCChecker interface {
	CheckIngestionRPC(context.Context, []AssetRef, IngestionRPCCheckOptions) (IngestionRPCCheckResult, error)
}

type IngestionRPCRoleCoverage struct {
	Role              string `json:"role"`
	HealthyProviders  int    `json:"healthyProviders"`
	RequiredProviders int    `json:"requiredProviders"`
}
