// Command crypto-server runs the Metapus API server for the crypto-processing
// configuration with the client extensions.
package main

import (
	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

// Version information — set via ldflags at build time:
//
//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/crypto-server
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	launch.Server(launch.Build{Version: Version, BuildTime: BuildTime}, content.Crypto(content.Services{}), launch.WithExtensions(extensions.Register))
}
