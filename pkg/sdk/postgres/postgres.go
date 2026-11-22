// Package postgres is the platform's PostgreSQL storage a configuration's
// repositories use: the tenant's transaction manager, reference
// resolution, document locks, the event log and the outbox.
package postgres

import (
	"context"

	"metapus/internal/application/readaccess"
	"metapus/internal/core/security"
	"metapus/internal/domain/eventlog"
	"metapus/internal/domain/filter"
	core "metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/metadata"
)

type (
	// BackoffStrategy computes the next retry delay based on the current retry count (0-indexed).
	// Implementations must be stateless — the retry count is the only input.
	BackoffStrategy = core.BackoffStrategy
	// MarkedObjectsRepo implements domain.MarkedObjectsProcessor: it plans and
	// performs physical deletion of deletion-marked catalogs and documents.
	//
	// References come from two places. The metadata registry names reference
	// fields, including those without a foreign key. Foreign keys in the schema
	// name the rest: a key from a table the registry does not describe still
	// blocks deletion, since PostgreSQL would refuse the DELETE.
	MarkedObjectsRepo = core.MarkedObjectsRepo
	// NullableRawAmount is the PostgreSQL adapter for nullable NUMERIC(78,0)
	// raw amount columns. Domain value objects remain unaware of pgx nullable types.
	NullableRawAmount = core.NullableRawAmount
	// OutboxHandler processes outbox messages.
	OutboxHandler = core.OutboxHandler
	// OutboxMessage represents a message in the transactional outbox.
	OutboxMessage = core.OutboxMessage
	// Pool wraps pgxpool.Pool to provide a clean interface.
	Pool = core.Pool
	// GetConn returns transaction if in context, otherwise acquires from pool.
	// This allows repos to work both inside and outside transactions.
	Querier = core.Querier
	// RefDisplay is a lightweight representation of a referenced catalog entity.
	// Sent alongside the raw ID so the frontend can display human-readable names.
	RefDisplay = core.RefDisplay
	// RefFinderRepo scans all registered entity tables for references to a given object.
	// Uses metadata.Registry to discover TypedRef and FK reference fields,
	// then queries each relevant table dynamically.
	//
	// Analogous to 1C's "Найти ссылки на объект" processing.
	RefFinderRepo = core.RefFinderRepo
	// ReferenceResolver batch-resolves catalog IDs to display names.
	// It collects IDs per table, executes one SELECT per table, and returns a map.
	//
	// Usage:
	//
	//	resolver := NewReferenceResolver()
	//	resolver.Add("cat_counterparties", doc.CounterpartyID)
	//	resolver.Add("cat_merchants", doc.MerchantID)
	//	resolved, err := resolver.Resolve(ctx)
	//	supplierName := resolved["cat_counterparties"][doc.CounterpartyID.String()]
	ReferenceResolver = core.ReferenceResolver
	// ResolvedRefs maps table → id_string → RefDisplay.
	ResolvedRefs = core.ResolvedRefs
	// RetryPolicy controls retries for one outbox message without imposing
	// webhook-specific limits on unrelated outbox consumers.
	RetryPolicy = core.RetryPolicy
	// OutboxRecord is an outbox message whose payload and trace context the
	// caller has already fixed.
	OutboxRecord = core.OutboxRecord
)

// DefaultTxOptions returns production-safe defaults.
func DefaultTxOptions() core.TxOptions { return core.DefaultTxOptions() }

// ExtractDBColumns extracts all column names from struct "db" tags.
// It handles embedded structs (like entity.Catalog) recursively.
// This function is called once at initialization time, so reflection overhead is acceptable.
//
// Usage:
//
//	columns := ExtractDBColumns[currency.Currency]()
//	// Returns: ["id", "code", "name", "iso_code", "symbol", ...]
func ExtractDBColumns[T any]() []string { return core.ExtractDBColumns[T]() }

// ExtractFilterFields derives trusted filter storage semantics from actual Go
// types and db tags. An optional column whitelist restricts the exposed fields.
// It is used once when repositories are registered, never for each SQL row.
func ExtractFilterFields[T any](columns ...string) map[string]filter.FieldSpec {
	return core.ExtractFilterFields[T](columns...)
}

// IsUniqueViolation checks whether the error is a Postgres unique constraint violation (23505).
func IsUniqueViolation(err error) bool { return core.IsUniqueViolation(err) }

// MustGetTxManager returns *postgres.TxManager from context.
// It is meant for infrastructure code that needs access to GetQuerier()/GetTx().
//
// Domain code should depend only on internal/core/tx.Manager.
func MustGetTxManager(ctx context.Context) *core.TxManager { return core.MustGetTxManager(ctx) }

func NewAuthorizedReadService(reg *metadata.Registry, policies *security.PolicyEngine) *readaccess.Service {
	return core.NewAuthorizedReadService(reg, policies)
}

// NewBatchInserter creates a new batch inserter.
func NewBatchInserter(txManager *core.TxManager) *core.BatchInserter {
	return core.NewBatchInserter(txManager)
}

func NewCommandRepository() *core.CommandRepository { return core.NewCommandRepository() }

func NewContextTxManager() core.ContextTxManager { return core.NewContextTxManager() }

// NewDocLocker creates a new DocLocker.
func NewDocLocker() *core.DocLocker { return core.NewDocLocker() }

// NewEventLogRepo creates a new event log repository.
func NewEventLogRepo() *core.EventLogRepo { return core.NewEventLogRepo() }

// NewMarkedObjectsRepo creates a MarkedObjectsRepo. events may be nil; when
// set, every physical deletion is recorded in the same transaction.
func NewMarkedObjectsRepo(registry *metadata.Registry, events eventlog.Writer) *core.MarkedObjectsRepo {
	return core.NewMarkedObjectsRepo(registry, events)
}

func NewNotificationRepo() *core.NotificationRepo { return core.NewNotificationRepo() }

// NewOutboxPublisher creates a new outbox publisher.
func NewOutboxPublisher() *core.OutboxPublisher { return core.NewOutboxPublisher() }

// NewRefFinderRepo creates a new RefFinderRepo.
func NewRefFinderRepo(registry *metadata.Registry) *core.RefFinderRepo {
	return core.NewRefFinderRepo(registry)
}

// NewReferenceResolver creates a new resolver.
func NewReferenceResolver() *core.ReferenceResolver { return core.NewReferenceResolver() }

func ReadSnapshot(ctx context.Context, read func(context.Context) error) error {
	return core.ReadSnapshot(ctx, read)
}

// SerializableTxOptions for critical operations requiring serializable isolation.
func SerializableTxOptions() core.TxOptions { return core.SerializableTxOptions() }

// WebhookBackoff returns interval deltas that place attempts at approximately
// 0, 30s, 2m, 10m, 1h, 6h, 12h, 24h, 48h and 72h from event creation.
func WebhookBackoff() core.BackoffStrategy { return core.WebhookBackoff() }

func WebhookRetryPolicy(msg *core.OutboxMessage) core.RetryPolicy {
	return core.WebhookRetryPolicy(msg)
}

// EnqueueOutbox writes pending messages in the caller's transaction, all or
// none.
func EnqueueOutbox(ctx context.Context, records ...OutboxRecord) error {
	return core.EnqueueOutbox(ctx, records...)
}

// PendingOutboxPayloadValues lists the payload field's values of the
// aggregate type's messages that are not yet delivered.
func PendingOutboxPayloadValues(ctx context.Context, aggregateType, field string) ([]string, error) {
	return core.PendingOutboxPayloadValues(ctx, aggregateType, field)
}
