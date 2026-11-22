// Package configuration is the contract between the platform and an industry
// configuration: a configuration implements Configuration and hands it to
// the platform's commands (server, worker, tenant, seed).
package configuration

import (
	"io/fs"

	dbassets "metapus/db"
	"metapus/extensions"
	core "metapus/internal/configuration"
	"metapus/internal/domain/auth"
	"metapus/internal/infrastructure/http/contracts/spec"
	"metapus/internal/infrastructure/http/v1"
)

type (
	// Configuration is an industry configuration built on the platform.
	Configuration = core.Configuration

	ServerEnv = core.ServerEnv
	Server    = core.Server
	WorkerEnv = core.WorkerEnv
	Worker    = core.Worker
	SeedEnv   = core.SeedEnv

	TenantEnv    = core.TenantEnv
	TenantWorker = core.TenantWorker
	OutboxRoute  = core.OutboxRoute
	Component    = core.Component
	Cleanup      = core.Cleanup
)

// EmbeddedMigrations returns the migrations of configuration name that the
// platform embeds from db/configuration/<name>/, for a configuration kept in
// the platform's repository. One kept in its own repository embeds its own.
func EmbeddedMigrations(name string) (fs.FS, error) { return dbassets.ConfigurationMigrations(name) }

// Compose assembles the application's factory registry: the platform's
// shared entities (platformcontent), the configuration's, then each client
// extension (extensions.Register). The server and the worker both start from
// it, so a dataset an extension registers is served over HTTP and run by
// scheduled reports alike.
func Compose(cfg core.Configuration, extensions ...func(*v1.FactoryRegistry)) *v1.FactoryRegistry {
	return core.Compose(cfg, extensions...)
}

// HTTPArtifacts generates the frontend wire files: the platform's (with
// client extensions) and, when the configuration declares contracts of its
// own, its files in frontend/configuration/<name>/, which import the
// platform types they use instead of declaring them again. count is the
// number of contracts generated.
func HTTPArtifacts(cfg core.Configuration, extensions ...func(*v1.FactoryRegistry)) ([]core.HTTPArtifact, int, error) {
	return core.HTTPArtifacts(cfg, extensions...)
}

// HTTPContractBundles returns every wire contract of the assembled
// application: the platform's with client extensions, then the
// configuration's. Route checks of the assembled server use it.
func HTTPContractBundles(cfg core.Configuration, extensions ...func(*v1.FactoryRegistry)) []spec.Bundle {
	return core.HTTPContractBundles(cfg, extensions...)
}

// Permissions returns every permission the assembled application's entities
// and reports require (v1.FactoryRegistry.Permissions), followed by the
// platform's system administration permissions (auth.SystemPermissions).
// Migration runs sync them into each tenant.
func Permissions(cfg core.Configuration, extensions ...func(*v1.FactoryRegistry)) []auth.Permission {
	return core.Permissions(cfg, extensions...)
}

// Extensions registers the client extension modules the platform's server
// assembles with every configuration (Compose). Pass it where the assembled
// application's routes or contracts are checked.
func Extensions(reg *v1.FactoryRegistry) { extensions.Register(reg) }
