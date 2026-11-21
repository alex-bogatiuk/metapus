package content

import (
	"io/fs"

	dbassets "metapus/db"
	"metapus/internal/configuration"
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/http/contracts/spec"
	"metapus/internal/infrastructure/http/cryptohttp"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/storage/postgres/crypto_integration_repo"
)

// CryptoName is the crypto-processing configuration's name.
const CryptoName = "crypto"

// Crypto returns the crypto-processing configuration. svc are the runtime
// services some of its documents use (Services); the zero value describes the
// configuration for contract generation, permission sync and migrations.
func Crypto(svc Services) configuration.Configuration { return cryptoConfiguration{services: svc} }

type cryptoConfiguration struct{ services Services }

func (cryptoConfiguration) Name() string { return CryptoName }

func (c cryptoConfiguration) Register(reg *v1.FactoryRegistry) { register(reg, c.services) }

func (cryptoConfiguration) RouteContracts() []spec.Bundle {
	return []spec.Bundle{cryptohttp.HTTPContracts()}
}

func (cryptoConfiguration) Migrations() fs.FS {
	migrations, err := fs.Sub(dbassets.Files, "configuration/"+CryptoName)
	if err != nil {
		panic(err) // the path is a valid constant
	}
	return migrations
}

func (cryptoConfiguration) IntegrationCatalog() *integrations.Catalog {
	return cryptointegrations.Catalog()
}

func (cryptoConfiguration) PoolConsumers() []integrations.PoolConsumer {
	return []integrations.PoolConsumer{crypto_integration_repo.RPCPoolConsumer{}}
}
