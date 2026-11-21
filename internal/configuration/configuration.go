// Package configuration is the contract between the platform and an industry
// configuration (crypto processing, an auto parts store, ...). A
// configuration hands the platform its entities, wire contracts and
// migrations through Configuration; the platform's processes assemble the
// application from it and never import the configuration's packages.
package configuration

import (
	"io/fs"
	"regexp"

	"metapus/internal/core/entity"
	"metapus/internal/domain/auth"
	"metapus/internal/infrastructure/http/contracts/spec"
	v1 "metapus/internal/infrastructure/http/v1"
)

// Configuration is an industry configuration built on the platform.
//
// A value is cheap to build and has no side effects. One built without
// runtime services (the configuration's own constructor decides which) must
// still describe the whole configuration: contract generation, permission
// sync and migrations use it.
type Configuration interface {
	// Name identifies the configuration. It is a lowercase directory name
	// (see ValidName): db/configuration/<name>/ holds its migrations and
	// frontend/configuration/<name>/ its user interface.
	Name() string
	// Register adds the configuration's catalogs, documents, registers,
	// report datasets and dataset executors to reg.
	Register(reg *v1.FactoryRegistry)
	// RouteContracts are the wire contracts of the routes the configuration
	// serves besides its registered entities' (its v1.RouteModule
	// routes). The entities' contracts come from the registry.
	RouteContracts() []spec.Bundle
	// Migrations are the configuration's tenant migrations, files named
	// NNNNN_name.sql at the root, applied after the platform's. It is nil
	// when the configuration has no tables of its own.
	Migrations() fs.FS
}

var validName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidName reports whether name can name a configuration.
func ValidName(name string) bool { return validName.MatchString(name) }

// Compose assembles the application's factory registry: the configuration's
// entities, then each client extension (extensions.Register). The server and
// the worker both start from it, so a dataset an extension registers is
// served over HTTP and run by scheduled reports alike.
func Compose(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) *v1.FactoryRegistry {
	reg := v1.NewFactoryRegistry()
	cfg.Register(reg)
	for _, register := range extensions {
		register(reg)
	}
	return reg
}

// Permissions returns every permission the assembled application's entities
// and reports require (v1.FactoryRegistry.Permissions), followed by the
// platform's system administration permissions (auth.SystemPermissions).
// Migration runs sync them into each tenant.
func Permissions(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) []auth.Permission {
	return append(Compose(cfg, extensions...).Permissions(), auth.SystemPermissions()...)
}

// CatalogDefinitions returns the Definitions of every catalog of the
// assembled application; tenant migrations number their items stored without
// a code (migration.WithCatalogCodes).
func CatalogDefinitions(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) []entity.Definition {
	reg := Compose(cfg, extensions...)
	definitions := make([]entity.Definition, 0, len(reg.Catalogs()))
	for _, catalog := range reg.Catalogs() {
		definitions = append(definitions, catalog.EntityDefinition())
	}
	return definitions
}
