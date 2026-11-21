// Command server runs the Metapus API server for the platform alone: the empty
// configuration (none) with the client extensions. An industry configuration
// ships its own main (for example cmd/crypto-server).
package main

import (
	"metapus/extensions"
	"metapus/internal/bootstrap/server"
	"metapus/internal/configuration/none"
)

// Version information — set via ldflags at build time:
//
//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/server
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	server.Main(server.Build{Version: Version, BuildTime: BuildTime}, none.New(), extensions.Register)
}
