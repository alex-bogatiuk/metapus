// Package configuration is the contract between the platform and an industry
// configuration: a configuration implements Configuration and hands it to
// the platform's commands (server, worker, tenant, seed).
package configuration

import (
	"io/fs"

	dbassets "metapus/db"
	core "metapus/internal/configuration"
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
