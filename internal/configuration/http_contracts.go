package configuration

import (
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/contracts"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/contracts/spec"
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
	"github.com/alex-bogatiuk/metapus/internal/platformcontent"
)

// HTTPContractBundles returns every wire contract of the assembled
// application: the platform's with client extensions, then the base
// configuration's and the add-on's. Route checks of the assembled server use it.
func HTTPContractBundles(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) []spec.Bundle {
	bundles := PlatformHTTPContractBundles(cfg, extensions...)
	for _, layer := range Stack(cfg) {
		bundles = append(bundles, OwnHTTPContractBundles(layer)...)
	}
	return bundles
}

// PlatformHTTPContractBundles are the contracts of the platform's shared
// entities and of client extensions, which use the platform's generic UI.
// The generator writes them to the platform's frontend files.
func PlatformHTTPContractBundles(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) []spec.Bundle {
	cfg = assembled(cfg)
	reg := v1.NewFactoryRegistry()
	platformcontent.Register(reg, platformcontent.Integrations{Catalog: IntegrationCatalog(cfg), PoolConsumers: cfg.PoolConsumers()})
	for _, register := range extensions {
		register(reg)
	}
	return []spec.Bundle{reg.HTTPContracts()}
}

// OwnHTTPContractBundles are the configuration's own contracts: its
// entities' and its route modules'. The generator writes them to the
// configuration's frontend files.
func OwnHTTPContractBundles(cfg Configuration) []spec.Bundle {
	reg := v1.NewFactoryRegistry()
	cfg.Register(reg)
	return append([]spec.Bundle{reg.HTTPContracts()}, cfg.RouteContracts()...)
}

// HTTPArtifact is one generated frontend file, by its repository path.
type HTTPArtifact struct {
	Path string
	Data []byte
}

// HTTPArtifacts generates the frontend wire files: the platform's (with
// client extensions) and, when the configuration declares contracts of its
// own, its files in frontend/configuration/<name>/, which import the
// platform types they use instead of declaring them again. count is the
// number of contracts generated.
func HTTPArtifacts(cfg Configuration, extensions ...func(*v1.FactoryRegistry)) (artifacts []HTTPArtifact, count int, err error) {
	platformBundles := PlatformHTTPContractBundles(cfg, extensions...)
	platform := contracts.Registry(platformBundles...)
	declarations, err := contracts.Generate(platform)
	if err != nil {
		return nil, 0, err
	}
	schemas, err := contracts.GenerateSchemas(platform)
	if err != nil {
		return nil, 0, err
	}
	routes, err := contracts.GenerateRoutes(platform, contracts.Routes(platformBundles...))
	if err != nil {
		return nil, 0, err
	}
	artifacts = []HTTPArtifact{
		{"frontend/types/generated/http-contracts.ts", declarations},
		{"frontend/lib/generated/http-contract-schemas.ts", schemas},
		{"frontend/lib/generated/http-contract-routes.ts", routes},
	}

	ownBundles := OwnHTTPContractBundles(cfg)
	own := contracts.BundleContracts(ownBundles...)
	ownRoutes := contracts.BundleRoutes(ownBundles...)
	if len(own) == 0 && len(ownRoutes) == 0 {
		return artifacts, len(platform), nil
	}
	base, err := contracts.NewLayer(contracts.PlatformModules, platform)
	if err != nil {
		return nil, 0, err
	}
	root := "configuration/" + cfg.Name()
	modules := contracts.Modules{
		Declarations: "@/" + root + "/types/generated/http-contracts",
		Schemas:      "@/" + root + "/lib/generated/http-contract-schemas",
	}
	ownDeclarations, err := contracts.GenerateOn(base, modules, own, false)
	if err != nil {
		return nil, 0, err
	}
	ownSchemas, err := contracts.GenerateOn(base, modules, own, true)
	if err != nil {
		return nil, 0, err
	}
	ownRouteTable, err := contracts.GenerateRoutesOn(base, modules, own, ownRoutes)
	if err != nil {
		return nil, 0, err
	}
	return append(artifacts,
		HTTPArtifact{"frontend/" + root + "/types/generated/http-contracts.ts", ownDeclarations},
		HTTPArtifact{"frontend/" + root + "/lib/generated/http-contract-schemas.ts", ownSchemas},
		HTTPArtifact{"frontend/" + root + "/lib/generated/http-contract-routes.ts", ownRouteTable},
	), len(platform) + len(own), nil
}
