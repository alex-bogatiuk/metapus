// register.go — Entry point for the Vehicle client extension.
// Call Register() from cmd/server/main.go to enable the Vehicle catalog.
package vehicle

import (
	"metapus/internal/platform"
	v1 "metapus/internal/infrastructure/http/v1"
)

// Register adds all Vehicle extension entities to the factory registry.
// Call this after content.RegisterDefaults(reg) in main.go:
//
//	factoryReg := v1.NewFactoryRegistry()
//	content.RegisterDefaults(factoryReg)
//	vehicle.Register(factoryReg, platform.ExtensionConfig{})
func Register(reg *v1.FactoryRegistry, _ platform.ExtensionConfig) {
	reg.RegisterCatalog(&VehicleRegistration{})
}

