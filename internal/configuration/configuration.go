// Package configuration is the contract between the platform and an industry
// configuration (crypto processing, an auto parts store, ...). A
// configuration hands the platform its entities, wire contracts and
// migrations through Configuration; the platform's processes assemble the
// application from it and never import the configuration's packages.
package configuration

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/domain/auth"
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/contracts/spec"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
	"github.com/alex-bogatiuk/metapus/internal/platformcontent"
	"github.com/alex-bogatiuk/metapus/pkg/sdk"
)

// Configuration is an industry configuration built on the platform.
//
// A value is cheap to build and has no side effects. One built without
// runtime services (the configuration's own constructor decides which) must
// still describe the whole configuration: contract generation, permission
// sync and migrations use it.
type Configuration interface {
	// Name identifies the configuration. It is a lowercase directory name
	// (see ValidName): db/configuration/<name>/ holds its migrations and
	// frontend/configuration/<name>/ its user interface.
	Name() string
	// APIVersion is the version of the platform API (github.com/alex-bogatiuk/metapus/pkg/sdk) the
	// configuration is built for, sdk.APIVersion at its build. The platform
	// refuses to run a configuration it cannot serve (CheckAPIVersion).
	APIVersion() string
	// Register adds the configuration's catalogs, documents, registers,
	// report datasets and dataset executors to reg. The platform's shared
	// entities (platformcontent) are already registered.
	Register(reg *v1.FactoryRegistry)
	// RouteContracts are the wire contracts of the routes the configuration
	// serves besides its registered entities' (its v1.RouteModule
	// routes). The entities' contracts come from the registry.
	RouteContracts() []spec.Bundle
	// Migrations are the configuration's tenant migrations, files named
	// NNNNN_name.sql at the root, applied after the platform's. It is nil
	// when the configuration has no tables of its own.
	Migrations() fs.FS
	// IntegrationCatalog lists the application's integration kinds and
	// providers: the platform's (integrations.PlatformKinds,
	// PlatformProviders) and the configuration's, in the order the settings
	// UI shows them. It is nil when the configuration adds none.
	IntegrationCatalog() *integrations.Catalog
	// PoolConsumers are the configuration's catalogs that read integration
	// accounts through pools (integrations.PoolConsumer).
	PoolConsumers() []integrations.PoolConsumer
	// Server builds the configuration's part of the API server: its routes,
	// posting modules and the runtime services of its entities. It reads
	// the configuration's settings and fails when they are invalid.
	Server(ctx context.Context, env ServerEnv) (Server, error)
	// Worker builds the configuration's part of the background worker.
	Worker(ctx context.Context, env WorkerEnv) (Worker, error)
	// Seed adds the configuration's initial data to a migrated tenant
	// database after the platform's (the seed command): with env.Demo, its
	// demo data. It must be safe to run again.
	Seed(ctx context.Context, env SeedEnv) error
}

// CheckAPIVersion fails when the platform's API (sdk.APIVersion) cannot
// serve cfg: another major version, or an older minor one. Every platform
// command checks it before it starts.
func CheckAPIVersion(cfg Configuration) error {
	if err := sdk.Compatible(cfg.APIVersion()); err != nil {
		return fmt.Errorf("configuration %s: %w", cfg.Name(), err)
	}
	return nil
}

var validName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// ValidName reports whether name can name a configuration: a lowercase
// identifier of at most 40 characters. A tenant database keeps the goose
// history of the configuration's migrations in goose_db_version_<name>, so
// the names of the platform's and the extensions' histories are taken.
func ValidName(name string) bool {
	return validName.MatchString(name) && name != "platform" && name != "extensions"
}

// IntegrationCatalog is the application's integration catalog: the
// configuration's, or the platform's alone when it adds none.
func IntegrationCatalog(cfg Configuration) *integrations.Catalog {
	if catalog := cfg.IntegrationCatalog(); catalog != nil {
		return catalog
	}
	return integrations.PlatformCatalog()
}

// Compose assembles the application's factory registry: the platform's
// shared entities (platformcontent), the configuration's, then each client
// extension (extensions.Register). The server and the worker both start from
// it, so a dataset an extension registers is served over HTTP and run by
// scheduled reports alike.
func Compose(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) *v1.FactoryRegistry {
	reg := v1.NewFactoryRegistry()
	platformcontent.Register(reg, platformcontent.Integrations{Catalog: IntegrationCatalog(cfg), PoolConsumers: cfg.PoolConsumers()})
	cfg.Register(reg)
	for _, register := range extensions {
		register(reg)
	}
	return reg
}

// Permissions returns every permission the assembled application's entities
// and reports require (v1.FactoryRegistry.Permissions), followed by the
// platform's system administration permissions (auth.SystemPermissions).
// Migration runs sync them into each tenant.
func Permissions(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) []auth.Permission {
	return append(Compose(cfg, extensions...).Permissions(), auth.SystemPermissions()...)
}

// CatalogDefinitions returns the Definitions of every catalog of the
// assembled application; tenant migrations number their items stored without
// a code (migration.WithCatalogCodes).
func CatalogDefinitions(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) []entity.Definition {
	reg := Compose(cfg, extensions...)
	definitions := make([]entity.Definition, 0, len(reg.Catalogs()))
	for _, catalog := range reg.Catalogs() {
		definitions = append(definitions, catalog.EntityDefinition())
	}
	return definitions
}

// WithRegister is cfg registering its entities with register instead of its
// own Register; a nil register keeps cfg as it is. The server composes the
// configuration with the runtime services its part built (Server.Register).
func WithRegister(cfg Configuration, register func(*v1.FactoryRegistry)) Configuration {
	if register == nil {
		return cfg
	}
	return registered{Configuration: cfg, register: register}
}

type registered struct {
	Configuration
	register func(*v1.FactoryRegistry)
}

func (r registered) Register(reg *v1.FactoryRegistry) { r.register(reg) }
