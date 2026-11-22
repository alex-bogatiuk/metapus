// Package contracts holds the wire contracts of a configuration's own routes.
package contracts

import (
	"metapus/internal/core/entity"
	"metapus/internal/infrastructure/http/contracts"
	"metapus/internal/infrastructure/http/contracts/spec"
	"metapus/internal/metadata"
)

// Bundle is a set of HTTP route contracts.
type Bundle = spec.Bundle

type (
	Contract      = spec.Contract
	RouteContract = spec.RouteContract
)

// EntityWithDetail supports an explicit detail projection without changing the
// list DTO or granting additional mutation capabilities.
func EntityWithDetail[Create any, Update any, List any, Read any](def entity.Definition, caps metadata.DocumentCapabilities) spec.Bundle {
	return spec.EntityWithDetail[Create, Update, List, Read](def, caps)
}

func Request[T any](name string) spec.Contract { return spec.Request[T](name) }

func Response[T any](name string) spec.Contract { return spec.Response[T](name) }

// Registry combines the platform service DTOs with configuration bundles:
// entity factory bundles and configuration route-module bundles. Entity DTOs
// belong in their registration bundle.
func Registry(bundles ...spec.Bundle) []contracts.Contract { return contracts.Registry(bundles...) }

// Routes lists the platform route contracts followed by configuration bundles.
func Routes(bundles ...spec.Bundle) []contracts.RouteContract { return contracts.Routes(bundles...) }
