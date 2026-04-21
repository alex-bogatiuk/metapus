// Package v1 provides HTTP API version 1.
// factory_registry.go — Extensible registry for catalog and document factories.
// Replaces hardcoded var slices, enabling client extensions without core modification.
package v1

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/platform"
)

// RouteRegistration is a generic interface for route groups that don't follow
// the standard CRUD pattern (registers, reports). Each registration builds its
// handler and registers its own routes on the provided gin.RouterGroup.
type RouteRegistration interface {
	// RoutePrefix returns the URL path segment, e.g. "stock".
	RoutePrefix() string
	// RegisterRoutes builds the handler and mounts routes on the group.
	RegisterRoutes(group *gin.RouterGroup, cfg RouterConfig)
}

// FactoryRegistry is the extensible registry of all entity factories.
// Clients add their own factories via Register methods without modifying core files.
//
// Usage (composition root / main.go):
//
//	reg := v1.NewFactoryRegistry()
//	content.RegisterDefaults(reg)                     // built-in entities
//	reg.RegisterDocument(&custom.MyDocRegistration{}) // client extension
//
//	router := v1.NewRouter(v1.RouterConfig{
//	    Registry: reg,
//	    // ...
//	})
type FactoryRegistry struct {
	catalogs     []CatalogRegistration
	documents    []DocumentRegistration
	registers    []RouteRegistration
	reports      []RouteRegistration                // legacy (non-typed)
	typedReports []platform.ReportRouteAdapter       // new typed reports
}

// NewFactoryRegistry creates an empty registry.
func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{}
}

// RegisterCatalog adds a catalog factory to the registry.
func (r *FactoryRegistry) RegisterCatalog(reg CatalogRegistration) {
	r.catalogs = append(r.catalogs, reg)
}

// RegisterDocument adds a document factory to the registry.
func (r *FactoryRegistry) RegisterDocument(reg DocumentRegistration) {
	r.documents = append(r.documents, reg)
}

// RegisterRegister adds an accumulation register route registration.
func (r *FactoryRegistry) RegisterRegister(reg RouteRegistration) {
	r.registers = append(r.registers, reg)
}

// RegisterReport adds a report route registration (legacy, non-typed).
func (r *FactoryRegistry) RegisterReport(reg RouteRegistration) {
	r.reports = append(r.reports, reg)
}

// RegisterTypedReport wraps a typed ReportRegistration into the registry.
// The platform automatically creates:
//   - GET /reports/{prefix}          → Execute()
//   - GET /reports/{prefix}/metadata → Meta()
//   - RequirePermission(Permission())
func RegisterTypedReport[F any, R any](reg *FactoryRegistry, report platform.ReportRegistration[F, R]) {
	adapter := handlers.WrapReportRegistration(report)
	reg.typedReports = append(reg.typedReports, adapter)
}

// Catalogs returns all registered catalog factories.
func (r *FactoryRegistry) Catalogs() []CatalogRegistration {
	return r.catalogs
}

// Documents returns all registered document factories.
func (r *FactoryRegistry) Documents() []DocumentRegistration {
	return r.documents
}

// Registers returns all registered accumulation register route registrations.
func (r *FactoryRegistry) Registers() []RouteRegistration {
	return r.registers
}

// Reports returns all registered report route registrations (legacy).
func (r *FactoryRegistry) Reports() []RouteRegistration {
	return r.reports
}

// TypedReports returns all registered typed report adapters.
func (r *FactoryRegistry) TypedReports() []platform.ReportRouteAdapter {
	return r.typedReports
}
