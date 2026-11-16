package content

import (
	"metapus/internal/infrastructure/http/contracts"
	v1 "metapus/internal/infrastructure/http/v1"
)

// HTTPArtifact is one generated frontend file, by its repository path.
type HTTPArtifact struct {
	Path string
	Data []byte
}

// cryptoModules are the crypto configuration's generated frontend modules.
var cryptoModules = contracts.Modules{
	Declarations: "@/configuration/crypto/types/generated/http-contracts",
	Schemas:      "@/configuration/crypto/lib/generated/http-contract-schemas",
}

// HTTPArtifacts generates the frontend wire files: the platform's (with client
// extensions) and, on top of them, the crypto configuration's own, which
// import the platform types they use instead of declaring them again.
func HTTPArtifacts(register ...func(*v1.FactoryRegistry)) ([]HTTPArtifact, int, error) {
	platformBundles := PlatformHTTPContractBundles(register...)
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
	base, err := contracts.NewLayer(contracts.PlatformModules, platform)
	if err != nil {
		return nil, 0, err
	}
	cryptoBundles := CryptoHTTPContractBundles()
	crypto := contracts.BundleContracts(cryptoBundles...)
	cryptoDeclarations, err := contracts.GenerateOn(base, cryptoModules, crypto, false)
	if err != nil {
		return nil, 0, err
	}
	cryptoSchemas, err := contracts.GenerateOn(base, cryptoModules, crypto, true)
	if err != nil {
		return nil, 0, err
	}
	cryptoRoutes, err := contracts.GenerateRoutesOn(base, cryptoModules, crypto, contracts.BundleRoutes(cryptoBundles...))
	if err != nil {
		return nil, 0, err
	}
	return []HTTPArtifact{
		{"frontend/types/generated/http-contracts.ts", declarations},
		{"frontend/lib/generated/http-contract-schemas.ts", schemas},
		{"frontend/lib/generated/http-contract-routes.ts", routes},
		{"frontend/configuration/crypto/types/generated/http-contracts.ts", cryptoDeclarations},
		{"frontend/configuration/crypto/lib/generated/http-contract-schemas.ts", cryptoSchemas},
		{"frontend/configuration/crypto/lib/generated/http-contract-routes.ts", cryptoRoutes},
	}, len(platform) + len(crypto), nil
}
