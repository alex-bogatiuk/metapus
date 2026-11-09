package main

import (
	"log"
	"os"
	"vault-plugin-secrets-evm/internal/backend"

	"github.com/hashicorp/vault/sdk/plugin"
)

func main() {
	if err := plugin.Serve(&plugin.ServeOpts{BackendFactoryFunc: backend.Factory}); err != nil {
		log.Println("plugin shutting down", "error", err)
		os.Exit(1)
	}
}
