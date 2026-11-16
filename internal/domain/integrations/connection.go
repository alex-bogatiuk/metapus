package integrations

import "metapus/internal/core/id"

// ConnectionTarget is what a pool's connection check reads through. It
// carries configuration only, never resolved credentials. A pool read through
// a configuration's endpoint (for example an RPC connection) names it by
// EndpointID and URL; Attributes carry what that configuration's probe needs
// and are opaque to the platform.
type ConnectionTarget struct {
	PoolID     id.ID
	Provider   Provider
	EndpointID id.ID
	URL        string
	Attributes map[string]string
}
