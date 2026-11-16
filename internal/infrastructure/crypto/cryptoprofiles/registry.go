// Package cryptoprofiles holds the HTTP behaviour of the crypto
// configuration's integration providers: how each RPC operator takes its key,
// classifies failures and hints retries.
package cryptoprofiles

import (
	"sync"

	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/infrastructure/integrations/profiles"
)

// Profiles returns the configuration's provider profiles, paced as their
// provider definitions require.
func Profiles() []profiles.Profile {
	list := []profiles.Profile{alchemy(), infura(), drpc(), tronAPI(), chainstack(), getBlock()}
	for i := range list {
		definition, _ := cryptointegrations.Catalog().Definition(list[i].Provider)
		list[i].MinRequestIntervalMS = definition.AccountMinRequestIntervalMS
	}
	return list
}

var registry = sync.OnceValue(func() *profiles.Registry {
	r, err := profiles.New(append(profiles.Platform(), Profiles()...)...)
	if err != nil {
		panic(err)
	}
	return r
})

// Registry is the application's immutable profile registry: platform and
// crypto providers. Pass it to every transport and prober of this application.
func Registry() *profiles.Registry { return registry() }
