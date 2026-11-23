// Command autoparts-server runs the Metapus API server for the auto parts store
// configuration («Автозапчасти») with the client extensions.
package main

import (
	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/internal/bootstrap/server"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/content"
)

// Version information — set via ldflags at build time:
//
//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/autoparts-server
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	server.Main(server.Build{Version: Version, BuildTime: BuildTime}, content.Autoparts(), extensions.Register)
}
