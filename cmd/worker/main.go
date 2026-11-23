// Command worker runs the Metapus background worker for the platform alone: the empty
// configuration (none) with the client extensions. An industry configuration
// ships its own main (for example cmd/crypto-worker).
package main

import (
	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/internal/bootstrap/worker"
	"github.com/alex-bogatiuk/metapus/internal/configuration/none"
)

// Version information — set via ldflags at build time:
//
//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/worker
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	worker.Main(worker.Build{Version: Version, BuildTime: BuildTime}, none.New(), extensions.Register)
}
