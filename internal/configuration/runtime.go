package configuration

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/application/integrationapp"
	"metapus/internal/core/id"
	corenumerator "metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/core/workerjob"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/eventlog"
	"metapus/internal/domain/posting"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/pkg/logger"
)

// ServerEnv is what the API server hands a configuration that builds its
// part of the server (Configuration.Server). The configuration reads its own
// settings from the process environment.
type ServerEnv struct {
	Logger *logger.Logger
	// MetaPool is the control-plane database.
	MetaPool *pgxpool.Pool
	// Tenants resolves tenants by ID or public slug.
	Tenants   tenant.Registry
	Numerator corenumerator.Generator
	EventLog  eventlog.Writer
}

// Server is a configuration's part of the API server. The zero value adds
// nothing.
type Server struct {
	// Register replaces Configuration.Register in the server's registry:
	// the configuration's entities with the runtime services this part
	// built. nil registers with Configuration.Register.
	Register func(*v1.FactoryRegistry)
	// AccessScopes adds object scopes to sessions (auth.WithAccessScopeProvider).
	AccessScopes auth.AccessScopeProvider
	// PostingModules install the configuration's registers into posting.
	PostingModules []posting.Module
	// RouteModules mount the configuration's routes after the platform's.
	RouteModules []v1.RouteModule
	// IntegrationConnectionTests tests connections of the configuration's
	// integration providers from the settings UI.
	IntegrationConnectionTests *integrationapp.ConnectionTestService
}

// WorkerEnv is what the background worker hands a configuration that builds
// its part of the worker (Configuration.Worker).
type WorkerEnv struct {
	Logger *logger.Logger
	// MetaPool is the control-plane database.
	MetaPool *pgxpool.Pool
}

// Worker is a configuration's part of the background worker. The zero value
// adds nothing.
type Worker struct {
	// Tenant builds the configuration's part of one tenant's worker each
	// time the worker starts it. nil adds nothing to tenants.
	Tenant func(ctx context.Context, env TenantEnv) (TenantWorker, error)
}

// TenantEnv is one tenant's worker: its database and job journal.
type TenantEnv struct {
	Tenant *tenant.Tenant
	Pool   *pgxpool.Pool
	// Jobs records runs in the worker jobs journal (/admin/worker-jobs).
	Jobs *workerjob.Recorder
}

// TenantWorker is a configuration's part of one tenant's worker.
type TenantWorker struct {
	// CheckSchema reports a tenant database that lacks the tables the
	// configuration's worker part needs; the tenant is retried later.
	CheckSchema func(context.Context, *pgxpool.Pool) error
	// Outbox routes the tenant's outbox messages of an aggregate type to the
	// configuration's handler on a relay of their own; the platform's
	// automation relays skip them.
	Outbox []OutboxRoute
	// Components run as long as the tenant's worker does.
	Components []Component
	// Cleanups run every hour with the platform's.
	Cleanups []Cleanup
}

// OutboxRoute sends the outbox messages of one aggregate type to Handler.
// Handler may also be a postgres.OutboxBackoffProvider and a
// postgres.OutboxRetryPolicyProvider for those messages.
type OutboxRoute struct {
	// Job names the relay in the worker jobs journal.
	Job           string
	AggregateType string
	Handler       postgres.OutboxHandler
}

// Component is a long-running part of a tenant's worker. A supervised one
// is restarted with backoff when Run returns before the tenant stops, and
// reports its readiness (worker.component.ready) through ready.
type Component struct {
	Name       string
	Supervised bool
	Run        func(ctx context.Context, ready func(bool)) error
}

// Cleanup is an hourly housekeeping job; it returns the number of rows it
// handled.
type Cleanup struct {
	Job string
	Run func(context.Context) (int, error)
}

// SeedEnv is what the seed command hands a configuration that seeds its
// data (Configuration.Seed) after the platform's.
type SeedEnv struct {
	Logger *logger.Logger
	// Pool is the tenant database.
	Pool *postgres.Pool
	// AdminUserID is the administrator the platform seeded.
	AdminUserID id.ID
	// Demo asks for demo data (SEED_DEMO_DATA=true). The platform's is
	// seeded already: Currencies and RateSources hold its IDs.
	Demo bool
	// Currencies are the platform's demo currencies by ISO code.
	Currencies map[string]id.ID
	// RateSources are the platform's demo exchange rate sources by code.
	RateSources map[string]id.ID
}
