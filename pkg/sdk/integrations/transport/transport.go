// Package transport is the HTTP transport of integration providers: an
// account's binding and connection probes.
package transport

import (
	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/integrations"
)

type (
	Binding = core.Binding
	// ConnectionProber checks that a pool answers through its real binding.
	// Providers whose profile has a Probe are checked directly. For the others
	// the configuration supplies the request (EndpointProbe, which also rejects
	// providers it does not check) and, for a target bound to an endpoint, the
	// transport (EndpointTransport).
	ConnectionProber = core.ConnectionProber
	Transport        = core.Transport
)

var (
	ErrAccessDenied  = core.ErrAccessDenied
	ErrAttemptBudget = core.ErrAttemptBudget
	ErrUnavailable   = core.ErrUnavailable
)
