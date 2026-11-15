package contracts

import (
	"metapus/extensions"
	"metapus/internal/content"
	"metapus/internal/infrastructure/http/contracts/spec"
)

// configurationBundles is the full contract set the generator publishes.
func configurationBundles() []spec.Bundle {
	return content.HTTPContractBundles(extensions.Register)
}
