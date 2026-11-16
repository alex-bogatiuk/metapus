// Package v1 provides HTTP API version 1.
// factory_registry.go — Extensible registry for catalog and document factories.
// Replaces hardcoded var slices, enabling client extensions without core modification.
package v1

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"metapus/internal/core/entity"
	"metapus/internal/infrastructure/http/contracts/spec"
	"metapus/internal/infrastructure/readpolicy"
	"strings"

	"metapus/internal/domain/reports/schema"
)

// RouteRegistration is a generic interface for route groups that don't follow
// the standard CRUD pattern (registers, reports). Each registration builds its
// handler and registers its own routes on the provided gin.RouterGroup.
type RouteRegistration interface {
	// RoutePrefix returns the URL path segment, e.g. "settlement".
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
	datasetExecutors map[string]readpolicy.DatasetExecutor
	catalogs         []CatalogRegistration
	documents        []DocumentRegistration
	customDocuments  []CustomDocumentRegistration
	registers        []RouteRegistration
	datasets         []*schema.Dataset
}

// NewFactoryRegistry creates an empty registry.
func NewFactoryRegistry() *FactoryRegistry {
	return &FactoryRegistry{}
}

// RegisterCatalog adds a catalog factory to the registry.
func (r *FactoryRegistry) RegisterCatalog(reg CatalogRegistration) {
	r.validateEntity(reg, entity.CatalogKind)
	r.bind(reg)
	r.catalogs = append(r.catalogs, reg)
}

// RegisterDocument adds a document factory to the registry.
func (r *FactoryRegistry) RegisterDocument(reg DocumentRegistration) {
	r.validateEntity(reg, entity.DocumentKind)
	r.bind(reg)
	r.documents = append(r.documents, reg)
}

// RegisterCustomDocument adds a document-scoped workflow that does not expose
// the standard CRUD/post/unpost route set.
func (r *FactoryRegistry) RegisterCustomDocument(reg CustomDocumentRegistration) {
	r.validateEntity(reg, entity.DocumentKind)
	r.customDocuments = append(r.customDocuments, reg)
}

// bind lets a declared registration look up other registrations at Build.
func (r *FactoryRegistry) bind(reg any) {
	if b, ok := reg.(interface{ bindRegistry(*FactoryRegistry) }); ok {
		b.bindRegistry(r)
	}
}

type entityIdentity interface {
	entity.DefinitionProvider
	EntityName() string
	RoutePrefix() string
	Permission() string
}

func (r *FactoryRegistry) validateEntity(reg entityIdentity, kind entity.Kind) {
	if reg.EntityName() == "" || reg.RoutePrefix() == "" || !strings.HasPrefix(reg.Permission(), string(kind)+":") {
		panic(fmt.Sprintf("invalid %s registration: %q", kind, reg.EntityName()))
	}
	def := reg.EntityDefinition()
	if err := def.Validate(); err != nil {
		panic(err)
	}
	if def.Kind != kind || def.EntityName() != reg.EntityName() || def.RoutePrefix() != reg.RoutePrefix() || def.Permission() != reg.Permission() {
		panic("entity registration disagrees with its definition: " + reg.EntityName())
	}

	check := func(existing entityIdentity, existingKind entity.Kind) {
		if strings.EqualFold(existing.EntityName(), reg.EntityName()) || existing.Permission() == reg.Permission() || (existingKind == kind && existing.RoutePrefix() == reg.RoutePrefix()) {
			panic("duplicate entity registration: " + reg.EntityName())
		}
	}
	for _, existing := range r.catalogs {
		check(existing, entity.CatalogKind)
	}
	for _, existing := range r.documents {
		check(existing, entity.DocumentKind)
	}
	for _, existing := range r.customDocuments {
		check(existing, entity.DocumentKind)
	}
}

// RegisterRegister adds an accumulation register route registration.
func (r *FactoryRegistry) RegisterRegister(reg RouteRegistration) {
	r.registers = append(r.registers, reg)
}

// Catalogs returns all registered catalog factories.
func (r *FactoryRegistry) Catalogs() []CatalogRegistration {
	return r.catalogs
}

// Documents returns all registered document factories.
func (r *FactoryRegistry) Documents() []DocumentRegistration {
	return r.documents
}

// CustomDocuments returns all custom document workflow registrations.
func (r *FactoryRegistry) CustomDocuments() []CustomDocumentRegistration {
	return r.customDocuments
}

// Registers returns all registered accumulation register route registrations.
func (r *FactoryRegistry) Registers() []RouteRegistration {
	return r.registers
}

// RegisterDataset adds a declarative dataset to the registry.
func (r *FactoryRegistry) RegisterDataset(ds *schema.Dataset) {
	r.datasets = append(r.datasets, ds)
}

// Datasets returns all registered datasets.
func (r *FactoryRegistry) Datasets() []*schema.Dataset {
	return r.datasets
}

func (r *FactoryRegistry) RegisterDatasetExecutor(key string, executor readpolicy.DatasetExecutor) {
	if r.datasetExecutors == nil {
		r.datasetExecutors = make(map[string]readpolicy.DatasetExecutor)
	}
	if _, exists := r.datasetExecutors[key]; exists {
		panic("duplicate dataset executor: " + key)
	}
	r.datasetExecutors[key] = executor
}
func (r *FactoryRegistry) DatasetExecutors() map[string]readpolicy.DatasetExecutor {
	result := make(map[string]readpolicy.DatasetExecutor, len(r.datasetExecutors))
	for key, executor := range r.datasetExecutors {
		result[key] = executor
	}
	return result
}

// HTTPContracts collects each registered module's wire boundary. There is no
// separate catalog/document list in the generator.
func (r *FactoryRegistry) HTTPContracts() spec.Bundle {
	var result spec.Bundle
	add := func(provider spec.Provider) {
		bundle := provider.HTTPContracts()
		result.Contracts = append(result.Contracts, bundle.Contracts...)
		result.Routes = append(result.Routes, bundle.Routes...)
	}
	for _, registration := range r.Catalogs() {
		add(registration)
	}
	for _, registration := range r.Documents() {
		add(registration)
	}
	for _, registration := range r.CustomDocuments() {
		add(registration)
	}
	return result
}
