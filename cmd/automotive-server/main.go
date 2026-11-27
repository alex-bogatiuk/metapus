// Command automotive-server runs the Metapus API server for the add-on «Автобизнес»
// on «Торговля и сервис» with the client extensions.
package main

import (
	"github.com/alex-bogatiuk/metapus/configurations/automotive/content"
	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

// Version information — set via ldflags at build time:
//
//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/automotive-server
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	launch.Server(launch.Build{Version: Version, BuildTime: BuildTime}, content.Automotive(), launch.WithExtensions(extensions.Register))
}
