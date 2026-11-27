// Command autoparts-worker runs the Metapus background worker for the add-on «Автозапчасти»
// on «Торговля и сервис» with the client extensions.
package main

import (
	"github.com/alex-bogatiuk/metapus/configurations/autoparts/content"
	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

// Version information — set via ldflags at build time:
//
//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/autoparts-worker
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	launch.Worker(launch.Build{Version: Version, BuildTime: BuildTime}, content.AutoParts(), launch.WithExtensions(extensions.Register))
}
