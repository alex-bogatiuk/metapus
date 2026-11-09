package contracts

import (
	"metapus/extensions"
	"metapus/internal/content"
	"metapus/internal/infrastructure/http/contracts/spec"
	v1 "metapus/internal/infrastructure/http/v1"
)

func entityBundle() spec.Bundle {
	reg := v1.NewFactoryRegistry()
	content.RegisterDefaults(reg)
	extensions.Register(reg)
	return reg.HTTPContracts()
}
func entityContracts() []Contract   { return entityBundle().Contracts }
func entityRoutes() []RouteContract { return entityBundle().Routes }
