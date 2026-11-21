// Command crypto-worker runs the Metapus background worker for the crypto-processing
// configuration with the client extensions.
package main

import (
	"metapus/extensions"
	"metapus/internal/bootstrap/worker"
	"metapus/internal/configuration/crypto/content"
)

// Version information — set via ldflags at build time:
//
//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/crypto-worker
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	worker.Main(worker.Build{Version: Version, BuildTime: BuildTime}, content.Crypto(content.Services{}), extensions.Register)
}
