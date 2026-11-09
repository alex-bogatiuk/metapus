// Package extensions is the composition root for client entity modules.
// Register each module here once; server and HTTP generation use the same list.
package extensions

import (
	"metapus/internal/infrastructure/http/contracts/spec"
	v1 "metapus/internal/infrastructure/http/v1"
)

func Register(reg *v1.FactoryRegistry) {
	// Example: vehicle.Register(reg)
}

func HTTPContracts() spec.Bundle {
	reg := v1.NewFactoryRegistry()
	Register(reg)
	return reg.HTTPContracts()
}
