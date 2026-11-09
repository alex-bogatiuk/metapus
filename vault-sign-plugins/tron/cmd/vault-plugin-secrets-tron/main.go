package main

import (
	"log"
	"os"
	"vault-plugin-secrets-tron/internal/backend"

	"github.com/hashicorp/vault/sdk/plugin"
)

func main() {
	// Plugins are usually run as standalone processes and communicate via gRPC.
	// We rely on the Vault SDK to handle the lifecycle.
	apiClientMeta := &plugin.ServeOpts{
		BackendFactoryFunc: backend.Factory,
	}

	err := plugin.Serve(apiClientMeta)
	if err != nil {
		log.Println("plugin shutting down", "error", err)
		os.Exit(1)
	}
}
