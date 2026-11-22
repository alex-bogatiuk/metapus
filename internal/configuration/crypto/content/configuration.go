package content

import (
	"io/fs"

	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_integration_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/cryptoschema"
	"metapus/pkg/sdk/configuration"
	"metapus/pkg/sdk/contracts"
	"metapus/pkg/sdk/http/v1"
	"metapus/pkg/sdk/integrations"
)

// CryptoName is the crypto-processing configuration's name.
const CryptoName = cryptoschema.Name

// Crypto returns the crypto-processing configuration. svc are the runtime
// services some of its documents use (Services); the zero value describes the
// configuration for contract generation, permission sync and migrations.
func Crypto(svc Services) configuration.Configuration { return cryptoConfiguration{services: svc} }

type cryptoConfiguration struct{ services Services }

func (cryptoConfiguration) Name() string { return CryptoName }

// APIVersion: the crypto configuration is built for the platform API 3.1.
func (cryptoConfiguration) APIVersion() string { return "3.1.0" }

func (c cryptoConfiguration) Register(reg *v1.FactoryRegistry) { register(reg, c.services) }

func (cryptoConfiguration) RouteContracts() []contracts.Bundle {
	return []contracts.Bundle{cryptohttp.HTTPContracts()}
}

func (cryptoConfiguration) Migrations() fs.FS {
	return cryptoschema.Migrations()
}

func (cryptoConfiguration) IntegrationCatalog() *integrations.Catalog {
	return cryptointegrations.Catalog()
}

func (cryptoConfiguration) PoolConsumers() []integrations.PoolConsumer {
	return []integrations.PoolConsumer{crypto_integration_repo.RPCPoolConsumer{}}
}
