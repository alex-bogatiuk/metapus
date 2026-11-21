// Package none is the empty configuration: the platform with no industry
// part. It is the reference implementation of configuration.Configuration
// and the configuration platform tests assemble the application from when
// they need none of an industry's entities.
package none

import (
	"io/fs"

	"metapus/internal/configuration"
	"metapus/internal/infrastructure/http/contracts/spec"
	v1 "metapus/internal/infrastructure/http/v1"
)

// Name is the empty configuration's name.
const Name = "none"

type empty struct{}

// New returns the empty configuration.
func New() configuration.Configuration { return empty{} }

func (empty) Name() string                  { return Name }
func (empty) Register(*v1.FactoryRegistry)  {}
func (empty) RouteContracts() []spec.Bundle { return nil }
func (empty) Migrations() fs.FS             { return nil }
