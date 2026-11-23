// Package launch runs a configuration on the platform: a configuration's own
// cmd/<name>-server, -worker, -tenant and -seed mains are one call each.
//
//	func main() {
//		launch.Server(launch.Build{Version: Version, BuildTime: BuildTime},
//			content.MyConfiguration(), launch.WithExtensions(extensions.Register))
//	}
//
// Each function checks the configuration's APIVersion, reads the platform's
// environment and exits the process on a configuration or startup error.
package launch

import (
	"github.com/alex-bogatiuk/metapus/internal/bootstrap/launch"
	"github.com/alex-bogatiuk/metapus/internal/bootstrap/seed"
	"github.com/alex-bogatiuk/metapus/internal/bootstrap/server"
	"github.com/alex-bogatiuk/metapus/internal/bootstrap/tenant"
	"github.com/alex-bogatiuk/metapus/internal/bootstrap/worker"
	"github.com/alex-bogatiuk/metapus/internal/configuration"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
)

type (
	// Build identifies the running binary; mains set it from ldflags:
	//
	//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/<name>-server
	Build = launch.Build
	// Option adjusts how a command runs its configuration.
	Option = launch.Option
)

// WithExtensions adds client entity modules, registered after the
// configuration's own entities.
func WithExtensions(register ...func(*v1.FactoryRegistry)) Option {
	return launch.WithExtensions(register...)
}

// Server runs the API server for cfg until SIGINT/SIGTERM.
func Server(build Build, cfg configuration.Configuration, opts ...Option) {
	server.Main(build, cfg, opts...)
}

// Worker runs the background worker for cfg until SIGINT/SIGTERM.
func Worker(build Build, cfg configuration.Configuration, opts ...Option) {
	worker.Main(build, cfg, opts...)
}

// Tenant runs the tenant CLI for cfg: create, list, migrate and the rest of
// its subcommands, taken from the command line.
func Tenant(cfg configuration.Configuration, opts ...Option) { tenant.Main(cfg, opts...) }

// Seed seeds the tenant database at DATABASE_URL for cfg.
func Seed(cfg configuration.Configuration) { seed.Main(cfg) }
