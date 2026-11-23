// Package launch holds what the platform's commands take besides the
// configuration they run: the binary's build and the options a command's
// main passes. pkg/sdk/launch exposes them to a configuration's own mains.
package launch

import (
	v1 "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1"
)

// Build identifies the running binary; mains set it from ldflags:
//
//	go build -ldflags="-X main.Version=v1.3.0 -X main.BuildTime=2026-03-31T00:00:00Z" ./cmd/server
type Build struct {
	Version   string
	BuildTime string
}

// Option adjusts how a command runs its configuration.
type Option func(*Options)

// Options are the settings the options of a command's main add up to.
type Options struct {
	// Extensions register the client's entity modules after the
	// configuration's own entities.
	Extensions []func(*v1.FactoryRegistry)
}

// WithExtensions adds client entity modules, such as extensions.Register.
func WithExtensions(register ...func(*v1.FactoryRegistry)) Option {
	return func(o *Options) { o.Extensions = append(o.Extensions, register...) }
}

// Apply adds the options up in order.
func Apply(opts []Option) Options {
	var o Options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}
