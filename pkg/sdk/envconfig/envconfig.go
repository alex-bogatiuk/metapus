// Package envconfig reads configuration from the environment.
package envconfig

import (
	core "metapus/internal/platform/envconfig"
)

type (
	// Reader accumulates configuration errors across reads.
	Reader = core.Reader
)

// New returns an empty Reader.
func New() *core.Reader { return core.New() }
