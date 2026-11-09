package integrations

import "metapus/internal/core/id"

// ConnectionTarget carries configuration only, never resolved credentials.
type ConnectionTarget struct {
	PoolID                               id.ID
	Provider                             Provider
	EndpointID                           id.ID
	URL, Family, NetworkIdentifier, Role string
}
