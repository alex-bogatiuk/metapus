package platform

import "metapus/internal/domain/posting"

// ExtensionConfig holds shared cross-cutting services available to client
// extensions during registration. Built by the composition root (main.go).
//
// Note: entity registration (catalogs, documents) is done via the
// v1.FactoryRegistry passed alongside this config. ExtensionConfig provides
// access to services that extensions can't obtain through CatalogDeps/DocumentDeps.
//
// Usage in client extension:
//
//	func Register(reg *v1.FactoryRegistry, cfg platform.ExtensionConfig) {
//	    reg.RegisterCatalog(&VehicleRegistration{})
//	    if cfg.PostingEngine != nil {
//	        cfg.PostingEngine.AddVisitor(&FuelVisitor{})
//	    }
//	}
type ExtensionConfig struct {
	// PostingEngine allows adding custom visitors and recorders.
	// Nil if posting is not initialized (catalog-only extensions can ignore this).
	PostingEngine *posting.Engine
}


