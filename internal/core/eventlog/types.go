// Package eventlog provides domain types for the system event log
// (analogue of 1C EventLog).
package eventlog

import (
	"time"

	"metapus/internal/core/id"
)

// ---------------------------------------------------------------------------
// Category — event classification
// ---------------------------------------------------------------------------

// Category classifies events into high-level groups.
type Category string

const (
	CategorySession    Category = "session"
	CategoryData       Category = "data"
	CategorySecurity   Category = "security"
	CategoryBackground Category = "background"
	CategorySystem     Category = "system"
	CategoryAPI        Category = "api"
)

// ---------------------------------------------------------------------------
// Severity — event importance
// ---------------------------------------------------------------------------

// Severity indicates the importance of an event.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// ---------------------------------------------------------------------------
// EventType — typed constants for all known event types
// ---------------------------------------------------------------------------

// EventType is a typed string for event classification.
// Constants are defined in Go code; the DB column is VARCHAR(50).
type EventType string

// Session events
const (
	EventSessionLogin       EventType = "session.login"
	EventSessionLoginFailed EventType = "session.login_failed"
	EventSessionLogout      EventType = "session.logout"
	EventSessionRefresh     EventType = "session.token_refresh"
	EventSessionImpersonate EventType = "session.impersonate"
	EventSessionBruteForce  EventType = "session.brute_force"
)

// Data events — documents
const (
	EventDocumentCreate EventType = "document.create"
	EventDocumentUpdate EventType = "document.update"
	EventDocumentDelete EventType = "document.delete"
	EventDocumentPost   EventType = "document.post"
	EventDocumentUnpost EventType = "document.unpost"
)

// Data events — catalogs
const (
	EventCatalogCreate EventType = "catalog.create"
	EventCatalogUpdate EventType = "catalog.update"
	EventCatalogDelete EventType = "catalog.delete"
)

// Security events
const (
	EventSecurityPermissionDenied EventType = "security.permission_denied"
	EventSecurityRLSBlocked       EventType = "security.rls_blocked"
	EventSecurityCELDenied        EventType = "security.cel_denied"
	EventSecurityProfileChanged   EventType = "security.profile_changed"
)

// Business logic events
const (
	EventStockNegativeBalance EventType = "stock.negative_balance"
	EventNumeratorGenerated   EventType = "numerator.generated"
)

// API events
const (
	EventAPISlowRequest EventType = "api.slow_request"
	EventAPIError500    EventType = "api.error_500"
	EventAPIRateLimited EventType = "api.rate_limited"
)

// System events
const (
	EventSystemMigration EventType = "system.migration"
	EventSystemPanic     EventType = "system.panic"
	EventSystemStartup   EventType = "system.startup"
	EventSystemShutdown  EventType = "system.shutdown"
)

// ---------------------------------------------------------------------------
// Event — domain entity
// ---------------------------------------------------------------------------

// Event represents a single entry in the system event log.
type Event struct {
	ID           id.ID
	Category     Category
	Severity     Severity
	EventType    EventType
	Source       string
	SessionID    string
	UserID       string
	UserEmail    string // resolved via JOIN, not stored
	ClientIP     string
	EntityType   string
	EntityID     *id.ID
	EntityNumber string
	Message      string
	Details      map[string]any
	TraceID      string
	RequestID    string
	DurationMs   *int
	CreatedAt    time.Time
}

// ---------------------------------------------------------------------------
// Filter — query parameters for listing events
// ---------------------------------------------------------------------------

// Filter contains parameters for querying the event log.
type Filter struct {
	Categories []Category
	Severities []Severity
	EventType  string
	UserID     string
	EntityType   string
	EntityID     *id.ID
	EntityNumber string // exact match on entity_number column
	Source       string
	Search     string // trigram search on message
	TraceID    string
	DateFrom   *time.Time
	DateTo     *time.Time
	OrderBy    string
	Limit      int
}

// DefaultFilter returns sensible defaults.
func DefaultFilter() Filter {
	return Filter{
		OrderBy: "-created_at",
		Limit:   50,
	}
}

// ---------------------------------------------------------------------------
// Stats — aggregated counts for KPI cards
// ---------------------------------------------------------------------------

// StatsFilter limits the time range for statistics.
type StatsFilter struct {
	DateFrom *time.Time
	DateTo   *time.Time
}

// Stats contains aggregated event counts by severity.
type Stats struct {
	Total    int64 `json:"total"`
	Info     int64 `json:"info"`
	Warning  int64 `json:"warning"`
	Error    int64 `json:"error"`
	Critical int64 `json:"critical"`
}
